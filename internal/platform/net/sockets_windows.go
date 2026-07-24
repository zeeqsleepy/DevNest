//go:build windows

package net

import (
	"context"
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"

	"github.com/devnest/devnest/internal/errors"
)

// Windows answers this question through the IP Helper API, which hands back a
// table of rows with the owning process id already attached. No subprocess, no
// parsing of localised console output, and no privilege beyond what the user
// already has: sockets owned by other users appear with their PID, and only
// naming that process needs more.
//
// The calls are made through the lazy DLL machinery in syscall rather than
// through a dependency. Two functions and four row layouts is a smaller thing
// to own than another module in go.mod.
var (
	iphlpapi                = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUDPTable = iphlpapi.NewProc("GetExtendedUdpTable")
)

const (
	addressFamilyINET  = 2
	addressFamilyINET6 = 23

	// TCP_TABLE_OWNER_PID_LISTENER and UDP_TABLE_OWNER_PID: the listener-only
	// table is exactly what this package promises, so the filtering happens in
	// the kernel rather than over a larger copy.
	tcpTableOwnerPIDListener = 3
	udpTableOwnerPID         = 1

	errorInsufficientBuffer = 122
)

// mibTCPRowOwnerPID is MIB_TCPROW_OWNER_PID.
type mibTCPRowOwnerPID struct {
	state      uint32
	localAddr  uint32
	localPort  uint32
	remoteAddr uint32
	remotePort uint32
	owningPID  uint32
}

// mibTCP6RowOwnerPID is MIB_TCP6ROW_OWNER_PID.
type mibTCP6RowOwnerPID struct {
	localAddr     [16]byte
	localScopeID  uint32
	localPort     uint32
	remoteAddr    [16]byte
	remoteScopeID uint32
	remotePort    uint32
	state         uint32
	owningPID     uint32
}

// mibUDPRowOwnerPID is MIB_UDPROW_OWNER_PID.
type mibUDPRowOwnerPID struct {
	localAddr uint32
	localPort uint32
	owningPID uint32
}

// mibUDP6RowOwnerPID is MIB_UDP6ROW_OWNER_PID.
type mibUDP6RowOwnerPID struct {
	localAddr    [16]byte
	localScopeID uint32
	localPort    uint32
	owningPID    uint32
}

func listSockets(ctx context.Context, options SocketOptions) ([]Socket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sockets := make([]Socket, 0, 64)

	if options.wantsTCP() {
		for _, family := range []uint32{addressFamilyINET, addressFamilyINET6} {
			rows, err := tableBytes(procGetExtendedTCPTable, family, tcpTableOwnerPIDListener)
			if err != nil {
				return nil, err
			}
			sockets = append(sockets, decodeTCP(rows, family)...)
		}
	}

	if options.wantsUDP() {
		for _, family := range []uint32{addressFamilyINET, addressFamilyINET6} {
			rows, err := tableBytes(procGetExtendedUDPTable, family, udpTableOwnerPID)
			if err != nil {
				return nil, err
			}
			sockets = append(sockets, decodeUDP(rows, family)...)
		}
	}

	return sockets, nil
}

// tableBytes calls one of the table functions twice: once to learn the size,
// once to fill a buffer of it.
//
// The table can grow between the two calls, which is why the second call's
// "buffer too small" is retried rather than reported. A machine opening
// sockets while being asked about them is normal, not an error.
func tableBytes(procedure *syscall.LazyProc, family, class uint32) ([]byte, error) {
	const attempts = 4
	size := uint32(0)

	for attempt := 0; attempt < attempts; attempt++ {
		var buffer []byte
		var pointer uintptr
		if size > 0 {
			buffer = make([]byte, size)
			pointer = uintptr(unsafe.Pointer(&buffer[0]))
		}

		result, _, _ := procedure.Call(
			pointer,
			uintptr(unsafe.Pointer(&size)),
			0, // bOrder: sorting is this package's job, not the kernel's
			uintptr(family),
			uintptr(class),
			0,
		)

		switch result {
		case 0:
			if buffer == nil {
				// An empty table: nothing is listening on this family.
				return nil, nil
			}
			return buffer, nil
		case errorInsufficientBuffer:
			continue
		default:
			return nil, errors.New(errors.CodeInternal,
				"the Windows socket table could not be read (error %d)", result)
		}
	}

	return nil, errors.New(errors.CodeInternal,
		"the Windows socket table kept changing while it was being read").
		WithHint("run the command again")
}

// decodeTCP reads a table of TCP rows.
//
// The row count is the first four bytes and the rows follow it, packed. The
// arithmetic is done on the byte slice rather than by casting to a Go array,
// so a table larger than the fixed array size a cast would need is not a
// special case.
func decodeTCP(table []byte, family uint32) []Socket {
	rows, entries, width := rowsIn(table, tcpRowWidth(family))
	sockets := make([]Socket, 0, entries)

	for index := 0; index < entries; index++ {
		row := rows[index*width : (index+1)*width]

		if family == addressFamilyINET {
			var decoded mibTCPRowOwnerPID
			read(row, &decoded)
			sockets = append(sockets, Socket{
				Protocol: ProtocolTCP,
				Address:  ipv4(decoded.localAddr),
				Port:     swapPort(uint16(decoded.localPort)),
				PID:      int(decoded.owningPID),
			})
			continue
		}

		var decoded mibTCP6RowOwnerPID
		read(row, &decoded)
		sockets = append(sockets, Socket{
			Protocol: ProtocolTCP,
			Address:  net.IP(decoded.localAddr[:]).String(),
			Port:     swapPort(uint16(decoded.localPort)),
			IPv6:     true,
			PID:      int(decoded.owningPID),
		})
	}

	return sockets
}

func decodeUDP(table []byte, family uint32) []Socket {
	rows, entries, width := rowsIn(table, udpRowWidth(family))
	sockets := make([]Socket, 0, entries)

	for index := 0; index < entries; index++ {
		row := rows[index*width : (index+1)*width]

		if family == addressFamilyINET {
			var decoded mibUDPRowOwnerPID
			read(row, &decoded)
			sockets = append(sockets, Socket{
				Protocol: ProtocolUDP,
				Address:  ipv4(decoded.localAddr),
				Port:     swapPort(uint16(decoded.localPort)),
				PID:      int(decoded.owningPID),
			})
			continue
		}

		var decoded mibUDP6RowOwnerPID
		read(row, &decoded)
		sockets = append(sockets, Socket{
			Protocol: ProtocolUDP,
			Address:  net.IP(decoded.localAddr[:]).String(),
			Port:     swapPort(uint16(decoded.localPort)),
			IPv6:     true,
			PID:      int(decoded.owningPID),
		})
	}

	return sockets
}

// rowsIn splits a table into its header count and the row bytes after it,
// clamping the count to what the buffer can actually hold.
func rowsIn(table []byte, width int) (rows []byte, entries, rowWidth int) {
	const header = 4
	if len(table) < header || width == 0 {
		return nil, 0, width
	}

	entries = int(binary.LittleEndian.Uint32(table[:header]))
	rows = table[header:]

	if available := len(rows) / width; entries > available {
		entries = available
	}
	return rows, entries, width
}

func tcpRowWidth(family uint32) int {
	if family == addressFamilyINET {
		return int(unsafe.Sizeof(mibTCPRowOwnerPID{}))
	}
	return int(unsafe.Sizeof(mibTCP6RowOwnerPID{}))
}

func udpRowWidth(family uint32) int {
	if family == addressFamilyINET {
		return int(unsafe.Sizeof(mibUDPRowOwnerPID{}))
	}
	return int(unsafe.Sizeof(mibUDP6RowOwnerPID{}))
}

// read copies one row's bytes into the struct that describes it.
func read(row []byte, into any) {
	switch target := into.(type) {
	case *mibTCPRowOwnerPID:
		*target = *(*mibTCPRowOwnerPID)(unsafe.Pointer(&row[0]))
	case *mibTCP6RowOwnerPID:
		*target = *(*mibTCP6RowOwnerPID)(unsafe.Pointer(&row[0]))
	case *mibUDPRowOwnerPID:
		*target = *(*mibUDPRowOwnerPID)(unsafe.Pointer(&row[0]))
	case *mibUDP6RowOwnerPID:
		*target = *(*mibUDP6RowOwnerPID)(unsafe.Pointer(&row[0]))
	}
}

// ipv4 renders an address that the API stores in network byte order.
func ipv4(address uint32) string {
	octets := make([]byte, 4)
	binary.LittleEndian.PutUint32(octets, address)
	return net.IP(octets).String()
}
