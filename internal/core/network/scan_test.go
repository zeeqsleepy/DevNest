package network

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// fakePortProber answers TCP probes per port, so a scan test can script which
// ports are open, which are filtered, and which refuse. The shared fakeProber
// describes a sequence of attempts; a scan fires many attempts in parallel, and
// which worker takes which port is not under the test's control, so this fake
// keys its answers by the port instead.
type fakePortProber struct {
	mu         sync.Mutex
	openPorts  map[int]time.Duration
	filtered   map[int]bool
	addresses  []string
	resolveErr error
	probed     map[int]int
}

func (f *fakePortProber) Probe(_ context.Context, _ string, port int) (time.Duration, error) {
	f.mu.Lock()
	if f.probed == nil {
		f.probed = make(map[int]int)
	}
	f.probed[port]++
	f.mu.Unlock()

	if duration, ok := f.openPorts[port]; ok {
		return duration, nil
	}
	if f.filtered[port] {
		return 0, errors.New(errors.CodeTimeout, "no answer in time")
	}
	return 0, errors.New(errors.CodeNetwork, "connection refused")
}

func (f *fakePortProber) ResolveHost(_ context.Context, _ string) ([]string, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return f.addresses, nil
}

func TestScanReportsOpenPortsInOrder(t *testing.T) {
	prober := &fakePortProber{
		openPorts: map[int]time.Duration{
			443: 30 * time.Millisecond,
			22:  12 * time.Millisecond,
			80:  15 * time.Millisecond,
		},
		addresses: []string{"192.0.2.10"},
	}

	result, err := Scan(context.Background(), prober, ScanRequest{
		Host:  "example.com",
		Ports: []int{443, 22, 80},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if result.OpenCount != 3 {
		t.Errorf("OpenCount = %d, want 3", result.OpenCount)
	}
	// The report is ordered by port regardless of the request order.
	wantPorts := []int{22, 80, 443}
	for index, open := range result.Open {
		if open.Port != wantPorts[index] {
			t.Errorf("open[%d].Port = %d, want %d", index, open.Port, wantPorts[index])
		}
	}
	if result.Open[0].Service != "ssh" {
		t.Errorf("service for 22 = %q, want %q", result.Open[0].Service, "ssh")
	}
	if result.TotalPorts != 3 || result.ClosedCount != 0 || result.FilteredCount != 0 {
		t.Errorf("counts = %+v", result)
	}
}

// Open, closed, and filtered must add up to the total, because every port is
// probed exactly once. A port that stays silent until the timeout is filtered;
// a port that refuses is closed; only a completed connection is open.
func TestScanCountsClosedAndFilteredPorts(t *testing.T) {
	prober := &fakePortProber{
		openPorts: map[int]time.Duration{22: 10 * time.Millisecond},
		filtered:  map[int]bool{443: true, 8000: true},
		addresses: []string{"192.0.2.10"},
	}

	result, err := Scan(context.Background(), prober, ScanRequest{
		Host:  "example.com",
		Ports: []int{22, 443, 8000, 8080, 9092},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if result.OpenCount != 1 || result.ClosedCount != 2 || result.FilteredCount != 2 {
		t.Errorf("open = %d, closed = %d, filtered = %d",
			result.OpenCount, result.ClosedCount, result.FilteredCount)
	}
	if result.TotalPorts != 5 {
		t.Errorf("TotalPorts = %d, want 5", result.TotalPorts)
	}
	if result.OpenCount+result.ClosedCount+result.FilteredCount != result.TotalPorts {
		t.Error("the three counts do not add up to the total")
	}
}

func TestScanCallsTheMethodTCP(t *testing.T) {
	result, err := Scan(context.Background(), &fakePortProber{addresses: []string{"192.0.2.1"}}, ScanRequest{
		Host: "example.com", Ports: []int{22},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.Method != "tcp" {
		t.Errorf("Method = %q, want %q", result.Method, "tcp")
	}
}

// An empty port list means the curated common set, and the scan reports every
// port it was asked about rather than only the ones that answered.
func TestScanDefaultsToCommonPorts(t *testing.T) {
	result, err := Scan(context.Background(), &fakePortProber{addresses: []string{"192.0.2.1"}}, ScanRequest{
		Host: "example.com",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.TotalPorts != len(CommonPorts()) {
		t.Errorf("TotalPorts = %d, want %d", result.TotalPorts, len(CommonPorts()))
	}
}

// A host that cannot be resolved is the one case a scan treats as an error:
// scanning a name that does not exist answers questions about nothing, unlike
// ping, where an address literal can skip resolution entirely.
func TestScanTreatsAnUnresolvableHostAsNotFound(t *testing.T) {
	prober := &fakePortProber{resolveErr: errors.New(errors.CodeNotFound, "no such host")}

	_, err := Scan(context.Background(), prober, ScanRequest{
		Host: "no-such-host.invalid", Ports: []int{80},
	})
	assertCode(t, err, errors.CodeNotFound)
}

func TestScanRejectsInvalidPorts(t *testing.T) {
	for _, ports := range [][]int{{0}, {70000}, {22, 0}} {
		_, err := Scan(context.Background(), &fakePortProber{}, ScanRequest{
			Host: "example.com", Ports: ports,
		})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

func TestScanRejectsTooMuchConcurrency(t *testing.T) {
	_, err := Scan(context.Background(), &fakePortProber{}, ScanRequest{
		Host: "example.com", Ports: []int{80}, Concurrency: 1000,
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestScanProbesEachPortExactlyOnce(t *testing.T) {
	prober := &fakePortProber{
		openPorts: map[int]time.Duration{22: 5 * time.Millisecond, 80: 5 * time.Millisecond},
		addresses: []string{"192.0.2.1"},
	}

	// The duplicate 22 must be de-duplicated, not probed twice.
	result, err := Scan(context.Background(), prober, ScanRequest{
		Host: "example.com", Ports: []int{22, 22, 80}, Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.TotalPorts != 2 {
		t.Errorf("TotalPorts = %d, want 2 after de-duplication", result.TotalPorts)
	}
	for port, count := range prober.probed {
		if count != 1 {
			t.Errorf("port %d was probed %d times, want 1", port, count)
		}
	}
}

func TestScanStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Scan(ctx, &fakePortProber{}, ScanRequest{
		Host: "example.com", Ports: []int{80},
	})
	assertCode(t, err, errors.CodeCancelled)
}

func TestExpandPorts(t *testing.T) {
	tests := []struct {
		spec string
		want []int
	}{
		{"443", []int{443}},
		{"22,80,443", []int{22, 80, 443}},
		{"22, 80 , 443", []int{22, 80, 443}},
		{"8000-8010", []int{8000, 8001, 8002, 8003, 8004, 8005, 8006, 8007, 8008, 8009, 8010}},
		{"1-3,7,10-12", []int{1, 2, 3, 7, 10, 11, 12}},
		{"1-65535", []int{1, 2}},
	}

	for _, test := range tests {
		t.Run(test.spec, func(t *testing.T) {
			ports, err := ExpandPorts(test.spec)
			if test.spec == "1-65535" {
				if err != nil {
					t.Fatalf("ExpandPorts: %v", err)
				}
				if len(ports) != 65535 || ports[0] != 1 || ports[65534] != 65535 {
					t.Errorf("full range gave %d ports", len(ports))
				}
				return
			}
			if err != nil {
				t.Fatalf("ExpandPorts(%q): %v", test.spec, err)
			}
			if !reflect.DeepEqual(ports, test.want) {
				t.Errorf("ExpandPorts(%q) = %v, want %v", test.spec, ports, test.want)
			}
		})
	}
}

func TestExpandPortsRejectsBadSpecs(t *testing.T) {
	for _, spec := range []string{"", "abc", "0", "65536", "80-10", "80,,443", "1-2-3", "80 -", "-80"} {
		t.Run(spec, func(t *testing.T) {
			_, err := ExpandPorts(spec)
			if err == nil {
				t.Errorf("ExpandPorts(%q) returned no error", spec)
				return
			}
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestServiceFor(t *testing.T) {
	if got := ServiceFor(22); got != "ssh" {
		t.Errorf("ServiceFor(22) = %q, want %q", got, "ssh")
	}
	if got := ServiceFor(5432); got != "postgresql" {
		t.Errorf("ServiceFor(5432) = %q, want %q", got, "postgresql")
	}
	if got := ServiceFor(62000); got != "" {
		t.Errorf("ServiceFor(62000) = %q, want an empty string", got)
	}
}

// The default scan list is a published table, so a caller must not be able to
// corrupt it for every future scan by mutating the slice it was handed.
func TestCommonPortsReturnsACopy(t *testing.T) {
	first := CommonPorts()
	second := CommonPorts()
	if reflect.DeepEqual(first, second) == false {
		t.Fatal("two calls returned different lists")
	}
	first[0] = 1

	if got := CommonPorts(); sort.IntsAreSorted(got) == false || got[0] == 1 {
		t.Errorf("mutating the returned slice changed the table")
	}
}
