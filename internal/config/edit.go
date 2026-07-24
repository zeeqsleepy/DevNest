package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// The editors below work on the file's own lines rather than re-emitting it
// from a parsed model. A configuration file is hand-written and hand-commented,
// and a tool that reformats the whole thing to change one value is a tool people
// stop using for the same reason they stopped letting an IDE reformat on save.

// SetInText returns the file with one key set to a value, adding the key or its
// section when either is missing.
func SetInText(contents []byte, key string, value any) ([]byte, error) {
	section, name, err := splitKey(key)
	if err != nil {
		return nil, err
	}
	literal := name + " = " + Literal(value)

	lines := splitLines(string(contents))
	current := ""
	sectionEnd := -1

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := sectionHeading(trimmed); ok {
			if current == section {
				sectionEnd = index
			}
			current = heading
			continue
		}
		if current == section && keyOnLine(trimmed) == name {
			lines[index] = literal
			return []byte(joinLines(lines)), nil
		}
		if current == section && trimmed != "" {
			sectionEnd = index + 1
		}
	}

	if current == section && sectionEnd < 0 {
		sectionEnd = len(lines)
	}
	if sectionEnd >= 0 {
		return []byte(joinLines(insert(lines, sectionEnd, literal))), nil
	}

	// The section is not in the file at all.
	body := joinLines(lines)
	if body != "" && !strings.HasSuffix(body, "\n\n") {
		body = strings.TrimRight(body, "\n") + "\n\n"
	}
	return []byte(body + "[" + section + "]\n" + literal + "\n"), nil
}

// UnsetInText returns the file with one key removed, reporting whether it was
// there to remove.
func UnsetInText(contents []byte, key string) ([]byte, bool, error) {
	section, name, err := splitKey(key)
	if err != nil {
		return nil, false, err
	}

	lines := splitLines(string(contents))
	current := ""

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heading, ok := sectionHeading(trimmed); ok {
			current = heading
			continue
		}
		if current == section && keyOnLine(trimmed) == name {
			return []byte(joinLines(append(lines[:index:index], lines[index+1:]...))), true, nil
		}
	}
	return contents, false, nil
}

// Literal renders a value the way the configuration file writes it.
func Literal(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case []string:
		quoted := make([]string, 0, len(typed))
		for _, item := range typed {
			quoted = append(quoted, strconv.Quote(item))
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	default:
		return strconv.Quote(fmt.Sprint(typed))
	}
}

// Template renders an annotated configuration file holding the current
// defaults, which is what "devnest config init" writes.
//
// It is generated from the field table rather than kept as a fixture, so a key
// added to DevNest cannot be missing from the file it tells people to write.
func Template() []byte {
	defaults := Default()

	var file strings.Builder
	file.WriteString("# DevNest configuration.\n")
	file.WriteString("#\n")
	file.WriteString("# Every value here is the compiled default, so a key left as it is changes\n")
	file.WriteString("# nothing. Documentation: docs/configuration.md.\n")

	section := ""
	for _, f := range fields() {
		if f.section != section {
			section = f.section
			fmt.Fprintf(&file, "\n[%s]\n", section)
		}
		fmt.Fprintf(&file, "%s = %s\n", f.key, Literal(f.get(defaults)))
	}

	return []byte(file.String())
}

func splitKey(key string) (section, name string, err error) {
	section, name, found := strings.Cut(key, ".")
	if !found || section == "" || name == "" {
		return "", "", errors.New(errors.CodeInvalidInput,
			"%q is not a configuration key", key).
			WithHint("keys are written as section.key, for example general.output")
	}
	return section, name, nil
}

func sectionHeading(trimmed string) (string, bool) {
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
	}
	return "", false
}

// keyOnLine returns the key a line assigns, or an empty string for a comment,
// a blank line, or anything else.
func keyOnLine(trimmed string) string {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	name, _, found := strings.Cut(trimmed, "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(name)
}

// splitLines splits without inventing a trailing empty line for a file that
// ends in a newline, so a round trip through the editors leaves the file's
// ending as it found it.
func splitLines(contents string) []string {
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	contents = strings.TrimSuffix(contents, "\n")
	if contents == "" {
		return nil
	}
	return strings.Split(contents, "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func insert(lines []string, at int, line string) []string {
	if at > len(lines) {
		at = len(lines)
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, line)
	return append(out, lines[at:]...)
}
