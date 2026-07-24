package config

import (
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// This is a decoder for the subset of TOML that DevNest's configuration file
// uses: comments, single-level section headers, and keys whose values are a
// string, integer, float, boolean, or single-line array of those.
//
// Not supported, because the configuration schema has no use for them:
// nested tables, arrays of tables, dotted keys, multi-line strings, and
// datetimes. A file using them is rejected with the line number rather than
// being partially understood. If the schema ever needs them, replace this file
// with a full TOML library; nothing outside this file depends on the subset.

// entry is one key-value pair read from a configuration file.
type entry struct {
	section string
	key     string
	value   any
	file    string
	line    int
}

func (e entry) where() string {
	return e.file + ", line " + strconv.Itoa(e.line)
}

// byteOrderMark is what several Windows editors put at the start of a UTF-8
// file. Windows is the primary platform, so a hand-edited configuration file
// arriving with one is normal and must not be a parse error.
const byteOrderMark = "\uFEFF"

func parseTOML(file string, data []byte) ([]entry, error) {
	var entries []entry
	section := ""

	contents := strings.TrimPrefix(string(data), byteOrderMark)

	for number, raw := range strings.Split(contents, "\n") {
		line := number + 1
		text := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))

		switch {
		case text == "" || strings.HasPrefix(text, "#"):
			continue

		case strings.HasPrefix(text, "["):
			name, err := parseSection(text)
			if err != nil {
				return nil, parseError(file, line, err.Error())
			}
			section = name

		default:
			key, value, err := parseAssignment(text)
			if err != nil {
				return nil, parseError(file, line, err.Error())
			}
			if section == "" {
				return nil, parseError(file, line,
					"key "+key+" appears before any [section] header")
			}
			entries = append(entries, entry{
				section: section, key: key, value: value, file: file, line: line,
			})
		}
	}

	return entries, nil
}

func parseError(file string, line int, message string) error {
	return errors.New(errors.CodeParse, "%s, line %d: %s", file, line, message).
		WithHint("run \"devnest config validate\" after fixing, or delete the file to start from defaults")
}

func parseSection(text string) (string, error) {
	text = stripComment(text)
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return "", errors.New(errors.CodeParse, "malformed section header %q", text)
	}
	name := strings.TrimSpace(text[1 : len(text)-1])
	if name == "" {
		return "", errors.New(errors.CodeParse, "section header has no name")
	}
	if strings.ContainsAny(name, "[].") {
		return "", errors.New(errors.CodeParse,
			"nested sections are not supported, found %q", name)
	}
	return name, nil
}

func parseAssignment(text string) (string, any, error) {
	name, rest, found := strings.Cut(text, "=")
	if !found {
		return "", nil, errors.New(errors.CodeParse, "expected \"key = value\", found %q", text)
	}
	key := strings.TrimSpace(name)
	if key == "" {
		return "", nil, errors.New(errors.CodeParse, "assignment has no key")
	}
	if strings.Contains(key, ".") {
		return "", nil, errors.New(errors.CodeParse, "dotted keys are not supported, found %q", key)
	}

	value, err := parseValue(strings.TrimSpace(stripComment(rest)))
	if err != nil {
		return "", nil, err
	}
	return key, value, nil
}

func parseValue(text string) (any, error) {
	switch {
	case text == "":
		return nil, errors.New(errors.CodeParse, "assignment has no value")

	case strings.HasPrefix(text, "\""):
		return parseBasicString(text)

	case strings.HasPrefix(text, "'"):
		if !strings.HasSuffix(text, "'") || len(text) < 2 {
			return nil, errors.New(errors.CodeParse, "unterminated string %s", text)
		}
		return text[1 : len(text)-1], nil

	case strings.HasPrefix(text, "["):
		return parseArray(text)

	case text == "true":
		return true, nil

	case text == "false":
		return false, nil
	}

	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value, nil
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return value, nil
	}
	return nil, errors.New(errors.CodeParse,
		"unrecognised value %s, expected a quoted string, number, boolean, or array", text)
}

func parseBasicString(text string) (string, error) {
	if len(text) < 2 || !strings.HasSuffix(text, "\"") {
		return "", errors.New(errors.CodeParse, "unterminated string %s", text)
	}
	body := text[1 : len(text)-1]

	var out strings.Builder
	for index := 0; index < len(body); index++ {
		char := body[index]
		if char != '\\' {
			out.WriteByte(char)
			continue
		}
		index++
		if index >= len(body) {
			return "", errors.New(errors.CodeParse, "string ends with an incomplete escape")
		}
		switch body[index] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '"':
			out.WriteByte('"')
		case '\\':
			out.WriteByte('\\')
		default:
			return "", errors.New(errors.CodeParse,
				"unsupported escape \\%c", body[index])
		}
	}
	return out.String(), nil
}

func parseArray(text string) (any, error) {
	if !strings.HasSuffix(text, "]") {
		return nil, errors.New(errors.CodeParse,
			"unterminated array, multi-line arrays are not supported")
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	if body == "" {
		return []any{}, nil
	}

	items := make([]any, 0, 4)
	for _, part := range splitTop(body) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := parseValue(part)
		if err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, nil
}

// splitTop splits on commas that are not inside a quoted string.
func splitTop(text string) []string {
	var parts []string
	var current strings.Builder
	var quote byte
	escaped := false

	for index := 0; index < len(text); index++ {
		char := text[index]
		switch {
		case escaped:
			escaped = false
			current.WriteByte(char)
		case quote != 0 && char == '\\':
			escaped = true
			current.WriteByte(char)
		case quote != 0:
			if char == quote {
				quote = 0
			}
			current.WriteByte(char)
		case char == '"' || char == '\'':
			quote = char
			current.WriteByte(char)
		case char == ',':
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteByte(char)
		}
	}
	return append(parts, current.String())
}

// stripComment removes a trailing "# ..." that is not inside a quoted string.
func stripComment(text string) string {
	var quote byte
	escaped := false

	for index := 0; index < len(text); index++ {
		char := text[index]
		switch {
		case escaped:
			escaped = false
		case quote != 0 && char == '\\':
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '"' || char == '\'':
			quote = char
		case char == '#':
			return strings.TrimSpace(text[:index])
		}
	}
	return strings.TrimSpace(text)
}
