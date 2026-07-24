package cli

import (
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// sizeValue is a flag holding a byte count written with a unit, so that
// --min-size 10MB works and a bare number is not left meaning something the
// user has to look up.
type sizeValue struct {
	target *int64
	text   string
}

func newSizeValue(target *int64, initial int64) *sizeValue {
	*target = initial
	return &sizeValue{target: target}
}

func (s *sizeValue) String() string {
	if s == nil || s.text == "" {
		return "0"
	}
	return s.text
}

func (s *sizeValue) Set(value string) error {
	parsed, err := ParseSize(value)
	if err != nil {
		return err
	}
	*s.target = parsed
	s.text = value
	return nil
}

// durationMsValue is a flag holding a duration stored as milliseconds, so that
// --max-response 500ms works and the result travels through the request and
// the JSON output as an integer with its unit in the field name.
type durationMsValue struct {
	target *int64
	text   string
}

func newDurationMsValue(target *int64, initial int64) *durationMsValue {
	*target = initial
	return &durationMsValue{target: target}
}

func (d *durationMsValue) String() string {
	if d == nil || d.text == "" {
		return "0"
	}
	return d.text
}

func (d *durationMsValue) Set(value string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return errors.New(errors.CodeInvalidInput, "invalid duration %q", value).
			WithHint("expected a duration with a unit, for example 500ms, 2s, or 1m")
	}
	if parsed < 0 {
		return errors.New(errors.CodeInvalidInput, "duration %q is negative", value)
	}

	*d.target = parsed.Milliseconds()
	d.text = value
	return nil
}

// units are ordered longest suffix first, so "KB" is matched before "B".
var units = []struct {
	suffix string
	factor int64
}{
	{"PB", 1 << 50},
	{"TB", 1 << 40},
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"P", 1 << 50},
	{"T", 1 << 40},
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
	{"B", 1},
}

// ParseSize reads a byte count such as "512", "10KB", "1.5GB", or "2g".
//
// Units are binary, matching what filesystems report and what every other disk
// tool on the machine shows.
func ParseSize(value string) (int64, error) {
	text := strings.ToUpper(strings.TrimSpace(value))
	if text == "" {
		return 0, errors.New(errors.CodeInvalidInput, "an empty size was given")
	}

	for _, unit := range units {
		if !strings.HasSuffix(text, unit.suffix) {
			continue
		}
		number := strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
		if number == "" {
			break
		}
		return scale(number, unit.factor, value)
	}

	return scale(text, 1, value)
}

func scale(number string, factor int64, original string) (int64, error) {
	if whole, err := strconv.ParseInt(number, 10, 64); err == nil {
		return checkRange(whole*factor, original)
	}
	fraction, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, errors.New(errors.CodeInvalidInput, "invalid size %q", original).
			WithHint("expected a number with an optional unit, for example 512, 10KB, or 1.5GB")
	}
	return checkRange(int64(fraction*float64(factor)), original)
}

func checkRange(bytes int64, original string) (int64, error) {
	if bytes < 0 {
		return 0, errors.New(errors.CodeInvalidInput, "size %q is negative", original)
	}
	return bytes, nil
}
