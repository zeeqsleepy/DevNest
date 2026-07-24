//go:build darwin

package net

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// macOS is the one platform where this costs a subprocess.
//
// The kernel interface is libproc, and libproc needs cgo. DevNest builds with
// CGO_ENABLED=0 so that a release binary is static and cross-compiles from any
// machine, and giving that up for one command on one platform is the wrong
// trade. lsof ships with macOS, has been on every release for two decades, and
// its machine-readable output (-F) is a stable contract rather than a table
// that reflows.
//
// The -F fields asked for are p (pid), c (command), P (protocol), n (name),
// and the output is a stream of one-letter-prefixed lines grouped by process.
// Nothing is passed through a shell and every argument is a literal.

// lsofTimeout bounds the subprocess. lsof on a busy machine is not instant,
// and a listing that never returns is worse than one that says it gave up.
const lsofTimeout = 5 * time.Second

// lsofOutputLimit caps what is read back, so a pathological machine cannot
// exhaust memory through a listing command.
const lsofOutputLimit = 8 << 20

func listSockets(ctx context.Context, options SocketOptions) ([]Socket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	arguments := []string{"-nP", "-FpcPn", "-i"}
	if options.wantsTCP() && !options.wantsUDP() {
		arguments = append(arguments, "-sTCP:LISTEN")
	}

	output, err := runLSOF(ctx, arguments)
	if err != nil {
		return nil, err
	}

	return parseLSOF(output, options), nil
}

func runLSOF(ctx context.Context, arguments []string) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, lsofTimeout)
	defer cancel()

	path, err := exec.LookPath("lsof")
	if err != nil {
		return "", errors.Wrap(err, errors.CodeUnsupported,
			"listing sockets on macOS needs lsof, which is not on PATH").
			WithHint("lsof ships with macOS; check whether PATH has been narrowed")
	}

	command := exec.CommandContext(bounded, path, arguments...)
	var stdout bytes.Buffer
	command.Stdout = &stdout

	// lsof exits non-zero when it merely could not inspect every process,
	// which is the normal case for an unprivileged user. What it did manage to
	// print is still the answer, so the exit status is deliberately ignored
	// and only a timeout or a failure to start is reported.
	runErr := command.Run()
	if bounded.Err() != nil {
		return "", errors.Wrap(bounded.Err(), errors.CodeTimeout,
			"listing sockets timed out after %s", lsofTimeout)
	}
	if stdout.Len() == 0 && runErr != nil {
		return "", errors.Wrap(runErr, errors.CodeInternal, "cannot list sockets")
	}
	if stdout.Len() > lsofOutputLimit {
		return stdout.String()[:lsofOutputLimit], nil
	}

	return stdout.String(), nil
}

// parseLSOF reads the field output format.
//
// Lines beginning with p start a process block and every f block after it
// belongs to that process, which is why the pid is carried forward rather than
// read per socket.
func parseLSOF(output string, options SocketOptions) []Socket {
	sockets := make([]Socket, 0, 32)

	pid := 0
	protocol := ""

	for _, line := range strings.Split(output, "\n") {
		if len(line) < 2 {
			continue
		}

		switch value := line[1:]; line[0] {
		case 'p':
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				pid = parsed
			}
		case 'P':
			protocol = strings.ToLower(strings.TrimSpace(value))
		case 'n':
			socket, ok := parseLSOFName(value, protocol, pid)
			if !ok || !wanted(socket, options) {
				continue
			}
			sockets = append(sockets, socket)
		}
	}

	return sockets
}

func wanted(socket Socket, options SocketOptions) bool {
	switch socket.Protocol {
	case ProtocolTCP:
		return options.wantsTCP()
	case ProtocolUDP:
		return options.wantsUDP()
	default:
		return false
	}
}

// parseLSOFName reads an endpoint out of a name field.
//
// Listening sockets appear as "*:8080", "127.0.0.1:8080", or "[::1]:8080". A
// name holding "->" is an established connection rather than a listener and is
// skipped: this package lists what is bound, not what is talking.
func parseLSOFName(name, protocol string, pid int) (Socket, bool) {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, " (LISTEN)")

	if name == "" || strings.Contains(name, "->") {
		return Socket{}, false
	}
	if protocol != ProtocolTCP && protocol != ProtocolUDP {
		return Socket{}, false
	}

	address, rawPort := name, ""
	if strings.HasPrefix(name, "[") {
		closing := strings.LastIndex(name, "]")
		if closing < 0 {
			return Socket{}, false
		}
		address = name[1:closing]
		rawPort = strings.TrimPrefix(name[closing+1:], ":")
	} else {
		separator := strings.LastIndex(name, ":")
		if separator < 0 {
			return Socket{}, false
		}
		address, rawPort = name[:separator], name[separator+1:]
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil || port <= 0 {
		return Socket{}, false
	}

	ipv6 := strings.Contains(address, ":")
	if address == "*" {
		address = "0.0.0.0"
	}

	return Socket{
		Protocol: protocol,
		Address:  address,
		Port:     port,
		IPv6:     ipv6,
		PID:      pid,
	}, true
}
