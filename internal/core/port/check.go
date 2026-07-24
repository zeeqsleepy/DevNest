package port

import (
	"context"
)

// CheckRequest asks about one port.
type CheckRequest struct {
	Port int
	TCP  bool
	UDP  bool
}

// CheckResult answers it.
//
// InUse is the field a script reads, and the exit code carries the same answer
// so that nothing has to be parsed at all.
type CheckResult struct {
	Port      int        `json:"port"`
	InUse     bool       `json:"inUse"`
	Listeners []Listener `json:"listeners"`
}

// Check reports whether a port is in use, and by what.
//
// A port below 1024 is answered for without any flag. The default that hides
// system ports exists to keep a listing readable; a direct question about one
// port deserves a direct answer, and somebody asking about port 80 knows what
// port 80 is.
func Check(ctx context.Context, enumerator Enumerator, inspector Inspector, request CheckRequest) (CheckResult, error) {
	if err := ValidatePort(request.Port); err != nil {
		return CheckResult{}, err
	}

	listing, err := List(ctx, enumerator, inspector, ListRequest{
		TCP:           request.TCP,
		UDP:           request.UDP,
		IncludeSystem: true,
		Port:          request.Port,
	})
	if err != nil {
		return CheckResult{}, err
	}

	return CheckResult{
		Port:      request.Port,
		InUse:     len(listing.Listeners) > 0,
		Listeners: listing.Listeners,
	}, nil
}
