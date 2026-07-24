package net

import (
	"crypto/tls"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"time"
)

// phases records when each stage of a request happened.
//
// The trace callbacks fire on the transport's goroutines, so the mutex is not
// decoration. On a redirect chain each stage fires once per hop and the last
// value wins, which is what makes the reported breakdown describe the final
// request.
type phases struct {
	mutex sync.Mutex

	start        time.Time
	dnsStart     time.Time
	dnsDone      time.Time
	connectStart time.Time
	connectDone  time.Time
	tlsStart     time.Time
	tlsDone      time.Time
	firstByte    time.Time
}

func (p *phases) trace() *httptrace.ClientTrace {
	p.start = time.Now()

	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			p.record(&p.dnsStart)
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			p.record(&p.dnsDone)
		},
		ConnectStart: func(string, string) {
			p.record(&p.connectStart)
		},
		ConnectDone: func(string, string, error) {
			p.record(&p.connectDone)
		},
		TLSHandshakeStart: func() {
			p.record(&p.tlsStart)
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			p.record(&p.tlsDone)
		},
		GotFirstResponseByte: func() {
			p.record(&p.firstByte)
		},
	}
}

func (p *phases) record(field *time.Time) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	*field = time.Now()
}

func (p *phases) timing(total time.Duration) Timing {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	return Timing{
		DNSMs:       span(p.dnsStart, p.dnsDone),
		ConnectMs:   span(p.connectStart, p.connectDone),
		TLSMs:       span(p.tlsStart, p.tlsDone),
		FirstByteMs: span(p.start, p.firstByte),
		TotalMs:     total.Milliseconds(),
	}
}

// span reports a duration in milliseconds, or zero when the stage did not
// happen. A cached DNS answer or a plain HTTP request legitimately skips one.
func span(from, to time.Time) int64 {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return 0
	}
	return to.Sub(from).Milliseconds()
}

// sortHeaders orders headers by name so two runs against an unchanged server
// produce identical output. Go stores them in a map, whose iteration order is
// deliberately random.
func sortHeaders(headers []Header) {
	sort.Slice(headers, func(i, j int) bool {
		left, right := strings.ToLower(headers[i].Name), strings.ToLower(headers[j].Name)
		if left != right {
			return left < right
		}
		return headers[i].Value < headers[j].Value
	})
}
