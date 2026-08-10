package network

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// ScanRequest describes one port scan.
type ScanRequest struct {
	Host string
	// Ports are the ports to probe, sorted and de-duplicated by Scan. Empty
	// means CommonPorts, the curated default set.
	Ports []int
	// Concurrency bounds how many probes are in flight at once. The default is
	// 100, which finishes a common-ports scan quickly without looking like an
	// attack to whoever operates the far end.
	Concurrency int
	// ProbeTimeout bounds a single connection attempt. Silence past the
	// timeout is classified as filtered rather than closed, which is how a
	// firewall that drops packets differs from a host that refuses.
	ProbeTimeout time.Duration
}

// OpenPort is one port that answered the probe.
type OpenPort struct {
	Port    int    `json:"port"`
	Service string `json:"service,omitempty"`
	// ResponseMs is how long the connection took, not a measurement of the
	// service itself.
	ResponseMs int64 `json:"responseMs"`
}

// ScanResult summarises a scan.
//
// Method is always "tcp" and is reported rather than assumed, for the same
// reason Ping reports it: nobody should have to guess which question was
// asked. No SYN-scan, no half-open scan, nothing that needs a raw socket or
// elevated privileges — a refused connection is detected by taking the
// rejection a normal dial provokes.
type ScanResult struct {
	Host      string     `json:"host"`
	Addresses []string   `json:"addresses"`
	Method    string     `json:"method"`
	Open      []OpenPort `json:"open"`
	// OpenCount, ClosedCount, and FilteredCount always add up to TotalPorts,
	// because every port is probed exactly once.
	OpenCount     int   `json:"openCount"`
	ClosedCount   int   `json:"closedCount"`
	FilteredCount int   `json:"filteredCount"`
	TotalPorts    int   `json:"totalPorts"`
	DurationMs    int64 `json:"durationMs"`
}

// Scan probes the ports a host is listening on.
//
// This is a connect scan: each port gets a normal TCP connection attempt, and
// a success means the service accepted the connection. It deliberately does
// not use SYN-scanning or any other half-open technique, which needs raw
// sockets and therefore elevation on every supported platform, and DevNest
// never asks for elevation.
//
// Probes run in parallel under a worker pool rather than one after another, so
// scanning a range of ports stays quick. The concurrency is bounded by default
// because a scan that opens thousands of sockets at once looks like an attack
// to the machine it is pointed at — rude even when the user owns it.
//
// A port that stays silent until the probe timeout is filtered, and a port
// whose connection attempt is refused is closed. A host that answers nothing
// at all therefore reports a number of filtered ports rather than a crash, and
// the run succeeds; a host that cannot be resolved is the one case treated as
// an error, since scanning a name that does not exist answers questions about
// nothing.
func Scan(ctx context.Context, prober Prober, request ScanRequest) (ScanResult, error) {
	host, err := ParseHost(request.Host)
	if err != nil {
		return ScanResult{}, err
	}

	ports := request.Ports
	if len(ports) == 0 {
		ports = CommonPorts()
	}
	ports = normalisePorts(ports)
	if len(ports) == 0 {
		return ScanResult{}, errors.New(errors.CodeInvalidInput, "no ports to scan").
			WithHint("pass a list like --ports 22,80,443 or a range like --ports 8000-8010")
	}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return ScanResult{}, errors.New(errors.CodeInvalidInput,
				"invalid port %d", port).
				WithHint("expected values between 1 and 65535")
		}
	}

	concurrency := request.Concurrency
	if concurrency <= 0 {
		concurrency = 100
	}
	if concurrency > 512 {
		return ScanResult{}, errors.New(errors.CodeInvalidInput,
			"%d concurrent probes is more than this command will open", concurrency).
			WithHint("a scan of 512 parallel connections already looks noisy at the far end")
	}

	probeTimeout := request.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 3 * time.Second
	}

	result := ScanResult{
		Host:       host,
		Addresses:  []string{},
		Method:     "tcp",
		Open:       []OpenPort{},
		TotalPorts: len(ports),
	}

	if err := ctx.Err(); err != nil {
		return ScanResult{}, errors.Wrap(err, errors.CodeCancelled, "cancelled")
	}

	// Everything that follows connects to the host, so the run stops here if
	// the host cannot be found. Unlike ping, there is no address-literal case
	// that would let a resolution failure be pushed off onto every probe: an
	// address literal resolves to itself.
	addresses, err := prober.ResolveHost(ctx, host)
	if err != nil {
		report := errors.Classify(err)
		if report.Code == errors.CodeCancelled {
			return ScanResult{}, err
		}
		return ScanResult{}, errors.Wrap(err, errors.CodeNotFound, "cannot resolve %s", host).
			WithHint("check the host name, and that this machine has working DNS")
	}
	result.Addresses = addresses

	startedAt := time.Now()

	job := make(chan int)
	var workers sync.WaitGroup
	workers.Add(concurrency)

	var (
		mutex    sync.Mutex
		open     []OpenPort
		closed   int
		filtered int
	)

	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case port, ok := <-job:
					if !ok {
						return
					}
					elapsed, probeErr := probe(ctx, prober, host, port, probeTimeout)
					mutex.Lock()
					switch {
					case probeErr == nil:
						open = append(open, OpenPort{
							Port:       port,
							Service:    ServiceFor(port),
							ResponseMs: elapsed.Milliseconds(),
						})
					case errors.CodeOf(probeErr) == errors.CodeTimeout:
						filtered++
					default:
						closed++
					}
					mutex.Unlock()
				}
			}
		}()
	}

dispatch:
	for _, port := range ports {
		select {
		case job <- port:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(job)
	workers.Wait()

	if err := ctx.Err(); err != nil {
		return ScanResult{}, errors.Wrap(err, errors.CodeCancelled, "cancelled")
	}

	sort.Slice(open, func(i, j int) bool { return open[i].Port < open[j].Port })

	result.Open = open
	result.OpenCount = len(open)
	result.ClosedCount = closed
	result.FilteredCount = filtered
	result.DurationMs = time.Since(startedAt).Milliseconds()

	return result, nil
}

// probe is one connection attempt, bounded by the probe timeout while still
// honouring the command's own deadline.
func probe(
	ctx context.Context,
	prober Prober,
	host string,
	port int,
	probeTimeout time.Duration,
) (time.Duration, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	return prober.Probe(probeCtx, host, port)
}

// normalisePorts sorts a port list and removes duplicates, so the same set of
// ports is probed in the same order every time and no port costs two probes.
func normalisePorts(ports []int) []int {
	seen := make(map[int]bool, len(ports))
	unique := make([]int, 0, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			unique = append(unique, port)
			continue
		}
		if !seen[port] {
			seen[port] = true
			unique = append(unique, port)
		}
	}
	sort.Ints(unique)
	return unique
}

// ExpandPorts parses a port specification into the ports it names.
//
// The syntax is numbers and inclusive ranges separated by commas: "22,80,443",
// "8000-8010", "1-1024", or any mix. It is deliberately not a full query
// language; a scanner needs a list of ports, and pretending it needs more is
// how jq-shaped complexity lands in a tool whose job is a TCP handshake.
func ExpandPorts(spec string) ([]int, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return nil, errors.New(errors.CodeInvalidInput, "no ports were given").
			WithHint("pass a list like --ports 22,80,443 or a range like --ports 8000-8010")
	}

	var ports []int
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New(errors.CodeInvalidInput,
				"invalid port list %q", spec).
				WithHint("expected numbers and ranges separated by commas")
		}

		if start, end, found := strings.Cut(part, "-"); found {
			from, err := parsePortPart(start)
			if err != nil {
				return nil, errors.New(errors.CodeInvalidInput,
					"invalid port range %q", part)
			}
			to, err := parsePortPart(end)
			if err != nil {
				return nil, errors.New(errors.CodeInvalidInput,
					"invalid port range %q", part)
			}
			if to < from {
				return nil, errors.New(errors.CodeInvalidInput,
					"invalid port range %q", part).
					WithHint("the range ends before it starts")
			}
			for port := from; port <= to; port++ {
				ports = append(ports, port)
			}
			continue
		}

		port, err := parsePortPart(part)
		if err != nil {
			return nil, errors.New(errors.CodeInvalidInput,
				"invalid port %q", part).
				WithHint("expected a value between 1 and 65535")
		}
		ports = append(ports, port)
	}

	return ports, nil
}

func parsePortPart(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if port < 1 || port > 65535 {
		return 0, errors.New(errors.CodeInvalidInput,
			"port %s is outside the range 1-65535", value)
	}
	return port, nil
}

// CommonPorts is the default scan set: the services a developer is most likely
// to be asking about. Scanning everything is possible, but it is an explicit
// choice rather than the default, because a sixty-five-thousand port sweep is
// noisy even when the machine being scanned belongs to the scanner.
var commonPorts = []int{
	21, 22, 23, 25, 53, 67, 68, 69, 80, 110, 111, 123, 135, 137, 139, 143,
	161, 179, 389, 443, 445, 465, 514, 587, 631, 636, 873, 993, 995, 1080,
	1433, 1521, 2049, 2181, 2375, 2376, 3000, 3306, 3389, 4369, 5000, 5432,
	5672, 5900, 5984, 6379, 6443, 7001, 8000, 8008, 8080, 8081, 8088, 8443,
	8888, 9000, 9092, 9200, 9418, 11211, 27017, 50000, 50070,
}

// CommonPorts returns a copy of the default scan list, so a caller mutating
// it cannot corrupt the table every future scan reads.
func CommonPorts() []int {
	return append([]int(nil), commonPorts...)
}

// serviceNames maps ports to the service that conventionally uses them. The
// list is a registry, not a detection: nothing connects to the service to
// confirm the guess, and a wrong guess is avoided by naming the table what it
// is in the docs.
var serviceNames = map[int]string{
	20: "ftp-data", 21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp",
	53: "dns", 67: "dhcp", 68: "dhcp", 69: "tftp", 80: "http",
	88: "kerberos", 110: "pop3", 111: "rpcbind", 123: "ntp", 135: "msrpc",
	137: "netbios-ns", 139: "netbios-ssn", 143: "imap", 161: "snmp",
	162: "snmptrap", 179: "bgp", 389: "ldap", 443: "https", 445: "smb",
	464: "kpasswd", 465: "smtps", 514: "syslog", 515: "lpd",
	587: "smtp-submission", 631: "ipp", 636: "ldaps", 873: "rsync",
	989: "ftps-data", 990: "ftps", 993: "imaps", 995: "pop3s",
	1080: "socks", 1194: "openvpn", 1433: "mssql", 1521: "oracle",
	1812: "radius", 1813: "radius-acct", 2049: "nfs", 2181: "zookeeper",
	2222: "ssh-alt", 2375: "docker", 2376: "docker-tls", 3000: "http-alt",
	3128: "squid", 3306: "mysql", 3389: "rdp", 4369: "rabbitmq",
	5000: "http-alt", 5432: "postgresql", 5672: "amqp", 5900: "vnc",
	5984: "couchdb", 6379: "redis", 6443: "kubernetes", 7001: "weblogic",
	8000: "http-alt", 8008: "http-alt", 8009: "ajp", 8080: "http-alt",
	8081: "http-alt", 8086: "influxdb", 8088: "http-alt", 8443: "https-alt",
	8888: "http-alt", 9000: "php-fpm", 9092: "kafka", 9100: "jetdirect",
	9200: "elasticsearch", 9300: "elasticsearch", 9418: "git", 11211: "memcached",
	12345: "netbus", 27017: "mongodb", 28017: "mongodb", 50000: "sap",
	50070: "hadoop-name-node", 61616: "activemq",
}

// ServiceFor returns the service conventionally associated with a port, or ""
// when DevNest does not know one.
func ServiceFor(port int) string {
	return serviceNames[port]
}
