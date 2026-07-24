package log

import "testing"

func TestDetectSeverityFindsTheLevel(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"2026-07-24T09:00:00Z ERROR database is unreachable", SeverityError},
		{"2026-07-24T09:00:00Z error database is unreachable", SeverityError},
		{"[WARN] cache miss rate is high", SeverityWarning},
		{"level=warning msg=\"disk filling up\"", SeverityWarning},
		{"FATAL out of memory", SeverityFatal},
		{"panic: runtime error", SeverityFatal},
		{"2026-07-24 CRIT power supply", SeverityFatal},
		{"an exception was thrown", SeverityError},
	}

	for _, test := range tests {
		got, ok := detectSeverity([]byte(test.line))
		if !ok {
			t.Errorf("no severity found in %q", test.line)
			continue
		}
		if got != test.want {
			t.Errorf("severity of %q = %q, want %q", test.line, got, test.want)
		}
	}
}

// The match is on whole tokens. A file name and a word that merely contains
// "error" are not reports of one.
func TestDetectSeverityIgnoresSubstrings(t *testing.T) {
	lines := []string{
		"2026-07-24T09:00:00Z INFO request completed in 41ms",
		"loaded errors.go with 41 handlers",
		"terrorist threat model reviewed",
		"the erroneous branch was removed",
		"",
	}

	for _, line := range lines {
		if severity, ok := detectSeverity([]byte(line)); ok {
			t.Errorf("%q was reported as %s", line, severity)
		}
	}
}

// A level named far into a stack trace is part of the trace, not a second
// report. Only the head of the line is examined.
func TestDetectSeverityOnlyLooksAtTheHeadOfTheLine(t *testing.T) {
	line := make([]byte, 0, levelWindow+64)
	for len(line) < levelWindow+8 {
		line = append(line, "goroutine stack frame "...)
	}
	line = append(line, " ERROR "...)

	if severity, ok := detectSeverity(line); ok {
		t.Errorf("a level %d bytes into the line was reported as %s", levelWindow, severity)
	}
}

func TestClassifyGroupsFailuresByCause(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"ERROR request timeout after 30s", CategoryTimeout},
		{"ERROR dial tcp 10.0.0.1:5432: connect: connection refused", CategoryConnection},
		{"ERROR permission denied writing /var/lib/cache", CategoryPermission},
		{"ERROR user 4821 not found", CategoryNotFound},
		{"ERROR duplicate key value violates constraint", CategoryDatabase},
		{"FATAL out of memory: cannot allocate 512 MB", CategoryMemory},
		{"ERROR panic: nil pointer dereference", CategoryCrash},
		{"ERROR syntax error near line 4", CategoryParse},
		{"ERROR x509: certificate has expired", CategoryTLS},
		{"ERROR no space left on device", CategoryDisk},
		{"ERROR something nobody has written a rule for", CategoryOther},
	}

	var buffer []byte
	for _, test := range tests {
		got, next := classify([]byte(test.line), buffer)
		buffer = next
		if got != test.want {
			t.Errorf("category of %q = %q, want %q", test.line, got, test.want)
		}
	}
}

// The grouping key is what makes "most common messages" useful: two reports of
// one problem differing only by an identifier have to land in one group.
func TestSignatureGroupsMessagesThatDifferOnlyByNumbers(t *testing.T) {
	first := string(signature([]byte("2026-07-24T09:04:02Z ERROR user 4821 not found"), nil))
	second := string(signature([]byte("2026-07-24T09:37:11Z ERROR user 9930 not found"), nil))

	if first != second {
		t.Errorf("signatures differ:\n  %q\n  %q", first, second)
	}

	other := string(signature([]byte("2026-07-24T09:04:02Z ERROR disk is full"), nil))
	if other == first {
		t.Errorf("two different failures produced one signature: %q", other)
	}
}

func TestSignatureIsCappedAndBufferIsReused(t *testing.T) {
	long := make([]byte, 0, signatureLength*4)
	for len(long) < signatureLength*3 {
		long = append(long, "abcdefghij "...)
	}

	buffer := make([]byte, 0, 8)
	got := signature(long, buffer)
	if len(got) > signatureLength {
		t.Errorf("signature is %d bytes, want at most %d", len(got), signatureLength)
	}

	// The buffer comes back for the next line, which is what keeps a scan over
	// ten million lines from allocating ten million times.
	again := signature([]byte("short line"), got)
	if string(again) != "short line" {
		t.Errorf("reused buffer produced %q", again)
	}
}
