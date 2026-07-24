package log

import "bytes"

// Severities reported by the error summary, most serious first.
const (
	SeverityFatal   = "fatal"
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// severities maps the tokens programs actually write to the three severities
// worth distinguishing.
//
// Adding a name is one entry. Everything else in the module works from the
// severity, so nothing else has to learn that "SEVERE" and "CRIT" mean the
// same thing as "FATAL".
var severities = map[string]string{
	"FATAL":     SeverityFatal,
	"PANIC":     SeverityFatal,
	"CRITICAL":  SeverityFatal,
	"CRIT":      SeverityFatal,
	"SEVERE":    SeverityFatal,
	"ERROR":     SeverityError,
	"ERR":       SeverityError,
	"EXCEPTION": SeverityError,
	"FAILURE":   SeverityError,
	"WARNING":   SeverityWarning,
	"WARN":      SeverityWarning,
}

// levelWindow is how far into a line the severity is looked for. Every logging
// library in use puts the level in the prefix; scanning the whole of a long
// stack trace for the word "error" finds it in a class name and calls the
// frame an error.
const levelWindow = 160

// maxToken is the longest severity name, and therefore the size of the buffer
// a token is folded into.
const maxToken = 9

// detectSeverity reports the severity a line announces, if any.
//
// Matching is case-insensitive and on whole tokens, so "error" and "[ERROR]"
// are found while "errors.go:41" and "terror" are not. The comparison folds
// into a stack array, so a hundred million lines cost no allocations.
func detectSeverity(line []byte) (string, bool) {
	if len(line) > levelWindow {
		line = line[:levelWindow]
	}

	var token [maxToken]byte
	length := 0
	flush := func() (string, bool) {
		found, known := "", false
		if length > 0 && length <= maxToken {
			found, known = severities[string(token[:length])]
		}
		length = 0
		return found, known
	}

	for _, character := range line {
		if isLetter(character) {
			if length < maxToken {
				token[length] = upper(character)
			}
			length++
			continue
		}
		if severity, found := flush(); found {
			return severity, true
		}
	}
	return flush()
}

func isLetter(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
}

func upper(character byte) byte {
	if character >= 'a' && character <= 'z' {
		return character - 32
	}
	return character
}

// Error categories. A category groups the failures that have the same cause
// and therefore the same fix, which is what someone reading a summary at two
// in the morning is trying to find.
const (
	CategoryTimeout     = "timeout"
	CategoryConnection  = "connection"
	CategoryPermission  = "permission"
	CategoryNotFound    = "not-found"
	CategoryDatabase    = "database"
	CategoryMemory      = "memory"
	CategoryCrash       = "crash"
	CategoryParse       = "parse"
	CategoryTLS         = "tls"
	CategoryDisk        = "disk"
	CategoryServerError = "server-error"
	CategoryOther       = "other"
)

// categoryRules are checked in order, so the more specific phrase wins. The
// table is the whole of the classification logic: adding a category is an
// entry here and nothing else.
var categoryRules = []struct {
	name     string
	keywords []string
}{
	{CategoryTimeout, []string{"timeout", "timed out", "deadline exceeded", "context deadline"}},
	{CategoryConnection, []string{
		"connection refused", "connection reset", "broken pipe", "no route to host",
		"connection closed", "econnrefused", "unreachable",
	}},
	{CategoryPermission, []string{
		"permission denied", "access denied", "unauthorized", "unauthorised",
		"forbidden", "invalid credentials", "authentication failed",
	}},
	{CategoryNotFound, []string{"not found", "no such file", "does not exist", "missing"}},
	{CategoryDatabase, []string{
		"database", "deadlock", "duplicate key", "constraint", "sqlstate",
		"too many connections", "query failed",
	}},
	{CategoryMemory, []string{"out of memory", "cannot allocate", "oom-kill", "heap"}},
	{CategoryCrash, []string{
		"nil pointer", "null pointer", "segmentation fault", "index out of range",
		"stack overflow", "panic:", "goroutine",
	}},
	{CategoryParse, []string{
		"parse error", "syntax error", "invalid character", "unmarshal",
		"malformed", "unexpected token", "decode",
	}},
	{CategoryTLS, []string{"certificate", "x509", "tls handshake", "ssl", "handshake failure"}},
	{CategoryDisk, []string{"no space left", "read-only file system", "i/o error", "disk full"}},
}

// classify names the category a message belongs to.
//
// The line is folded to lower case into a buffer the caller owns and reuses,
// so classifying every line of a large file allocates nothing. An unrecognised
// message is "other" rather than being dropped, because the count of failures
// nobody has written a rule for is itself worth seeing.
func classify(line []byte, buffer []byte) (string, []byte) {
	buffer = lower(line, buffer)
	for _, rule := range categoryRules {
		for _, keyword := range rule.keywords {
			if bytes.Contains(buffer, []byte(keyword)) {
				return rule.name, buffer
			}
		}
	}
	return CategoryOther, buffer
}

// lower folds a line to lower case into a reused buffer.
func lower(line []byte, buffer []byte) []byte {
	buffer = buffer[:0]
	if cap(buffer) < len(line) {
		buffer = make([]byte, 0, len(line))
	}
	for _, character := range line {
		if character >= 'A' && character <= 'Z' {
			character += 32
		}
		buffer = append(buffer, character)
	}
	return buffer
}

// signature reduces a message to a form that groups the repetitions of one
// problem together.
//
// Every run of digits becomes a hash mark, so "user 4821 not found" and
// "user 9930 not found" are one entry with a count of two rather than two
// entries with a count of one each. That collapse is the entire value of a
// "most common messages" listing: the raw lines are already in the file.
//
// The result is written into a buffer the caller reuses.
func signature(line []byte, buffer []byte) []byte {
	buffer = buffer[:0]

	digits := false
	space := false
	for _, character := range line {
		switch {
		case character >= '0' && character <= '9':
			if !digits {
				buffer = append(buffer, '#')
				digits = true
			}
			space = false
		case character == ' ' || character == '\t':
			digits = false
			if !space && len(buffer) > 0 {
				buffer = append(buffer, ' ')
				space = true
			}
		default:
			digits = false
			space = false
			buffer = append(buffer, character)
		}
		if len(buffer) >= signatureLength {
			break
		}
	}
	return bytes.TrimRight(buffer, " ")
}

// signatureLength caps how much of a message identifies it. Long enough that
// two different failures do not collide, short enough that a stack trace on
// one line does not become the key.
const signatureLength = 160
