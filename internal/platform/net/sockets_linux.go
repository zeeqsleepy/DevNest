//go:build linux

package net

import (
	"bufio"
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Linux publishes its socket tables as text under /proc/net, which gives the
// local address, the state, and the inode of each socket, but not the process
// holding it. The owner is found the other way round: every /proc/<pid>/fd
// entry that is a socket is a symlink reading "socket:[inode]", so one pass
// over the process table builds the inode-to-pid map.
//
// The second pass is the expensive half and it is only done when the first
// found something, because a machine with no listeners has nothing to attribute.
// Processes belonging to other users are unreadable without elevation; their
// sockets are still listed, with the PID left at zero.

// listenState is the value /proc/net/tcp writes for a listening socket.
const listenState = "0A"

// udpUnconnected is the state a bound UDP socket sits in. UDP has no listen
// state; a socket that is bound and has no peer is the closest equivalent, and
// it is what every other tool reports as a UDP listener.
const udpUnconnected = "07"

func listSockets(ctx context.Context, options SocketOptions) ([]Socket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type table struct {
		path     string
		protocol string
		state    string
		ipv6     bool
	}

	tables := make([]table, 0, 4)
	if options.wantsTCP() {
		tables = append(tables,
			table{"/proc/net/tcp", ProtocolTCP, listenState, false},
			table{"/proc/net/tcp6", ProtocolTCP, listenState, true},
		)
	}
	if options.wantsUDP() {
		tables = append(tables,
			table{"/proc/net/udp", ProtocolUDP, udpUnconnected, false},
			table{"/proc/net/udp6", ProtocolUDP, udpUnconnected, true},
		)
	}

	sockets := make([]Socket, 0, 64)
	// inodes runs alongside sockets: one entry each, holding the socket inode
	// the kernel reported, or an empty string where it reported none.
	inodes := make([]string, 0, 64)

	for _, source := range tables {
		found, foundInodes, err := readProcNet(source.path, source.protocol, source.state, source.ipv6)
		if err != nil {
			return nil, err
		}
		sockets = append(sockets, found...)
		inodes = append(inodes, foundInodes...)
	}

	attribute(sockets, inodes)
	return sockets, nil
}

// readProcNet parses one of the four socket tables.
//
// A table that does not exist is not an error: a kernel built without IPv6 has
// no /proc/net/tcp6, and a container may not expose every family. The absence
// of a table means no sockets of that kind, which is the truth.
func readProcNet(path, protocol, wanted string, ipv6 bool) ([]Socket, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, errors.Wrap(err, errors.CodeIO, "cannot read %s", path)
	}
	defer func() { _ = file.Close() }()

	sockets := make([]Socket, 0, 16)
	inodes := make([]string, 0, 16)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for line := 0; scanner.Scan(); line++ {
		if line == 0 {
			continue // the header
		}

		fields := strings.Fields(scanner.Text())
		const wantedFields = 10
		if len(fields) < wantedFields || fields[3] != wanted {
			continue
		}

		address, port, ok := parseProcAddress(fields[1], ipv6)
		if !ok {
			continue
		}

		sockets = append(sockets, Socket{
			Protocol: protocol,
			Address:  address,
			Port:     port,
			IPv6:     ipv6,
		})

		inode := fields[9]
		if inode == "0" {
			inode = ""
		}
		inodes = append(inodes, inode)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, errors.Wrap(err, errors.CodeIO, "cannot read %s", path)
	}

	return sockets, inodes, nil
}

// parseProcAddress decodes the "0100007F:1F90" form /proc uses.
//
// The address is hex, in the machine's own byte order per four-byte group,
// which is why an IPv4 address comes out reversed and an IPv6 address comes
// out in four reversed groups. The port is plain big-endian hex.
func parseProcAddress(field string, ipv6 bool) (string, int, bool) {
	rawAddress, rawPort, found := strings.Cut(field, ":")
	if !found {
		return "", 0, false
	}

	port, err := strconv.ParseUint(rawPort, 16, 32)
	if err != nil {
		return "", 0, false
	}

	decoded, err := hex.DecodeString(rawAddress)
	if err != nil {
		return "", 0, false
	}

	expected := net.IPv4len
	if ipv6 {
		expected = net.IPv6len
	}
	if len(decoded) != expected {
		return "", 0, false
	}

	for group := 0; group < len(decoded); group += 4 {
		reverse(decoded[group : group+4])
	}

	return net.IP(decoded).String(), int(port), true
}

func reverse(bytes []byte) {
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
}

// owners maps socket inodes to the process holding them.
//
// Anything unreadable is skipped rather than reported: a process belonging to
// another user, or one that exited between the directory listing and the read,
// are both normal and neither is a reason to fail a listing.
func owners(inodes map[string]bool) map[string]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	found := make(map[string]int, len(inodes))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		descriptors, err := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd"))
		if err != nil {
			continue
		}

		for _, descriptor := range descriptors {
			link, err := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", descriptor.Name()))
			if err != nil {
				continue
			}
			inode, ok := socketInode(link)
			if !ok {
				continue
			}
			if _, wanted := inodes[inode]; wanted {
				found[inode] = pid
			}
		}

		if len(found) == len(inodes) {
			break
		}
	}

	return found
}

// socketInode reads the inode out of a "socket:[12345]" symlink target.
func socketInode(link string) (string, bool) {
	const prefix = "socket:["
	if !strings.HasPrefix(link, prefix) || !strings.HasSuffix(link, "]") {
		return "", false
	}
	return link[len(prefix) : len(link)-1], true
}

// attribute writes the owning PID onto each socket whose inode was resolved.
//
// The two slices are parallel by construction: one entry appended to each per
// socket, in the same order, so the index is the association and no field has
// to be added to the exported type for a detail only Linux has.
func attribute(sockets []Socket, inodes []string) {
	wanted := make(map[string]bool, len(inodes))
	for _, inode := range inodes {
		if inode != "" {
			wanted[inode] = true
		}
	}
	if len(wanted) == 0 {
		return
	}

	found := owners(wanted)
	for index := range sockets {
		if index >= len(inodes) {
			break
		}
		if pid, ok := found[inodes[index]]; ok {
			sockets[index].PID = pid
		}
	}
}
