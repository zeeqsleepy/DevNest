package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/devnest/devnest/internal/errors"
)

// markdownRenderer writes a result as a report to paste into a ticket.
//
// It is generated from the same data the JSON renderer emits rather than from
// a per-command view, so a command cannot show one thing on the terminal and
// another in the report. That is possible because the JSON conventions in
// docs/export-system.md are strict enough to render from: a field whose name
// ends in "Bytes" is a size, one ending in "Ms" is a duration, a list of
// objects is a table, and camelCase is a sentence with the spaces removed.
//
// This is the one format where values are formatted for reading, because a
// person is the only consumer.
type markdownRenderer struct{}

func (markdownRenderer) Name() string { return "markdown" }

func (m markdownRenderer) Render(w io.Writer, envelope Envelope, _ TextFunc) error {
	var report strings.Builder

	title := envelope.DevNest.Command
	if title == "" {
		title = "devnest"
	}
	fmt.Fprintf(&report, "# %s report\n\n", sentence(title))
	fmt.Fprintf(&report, "Generated %s with DevNest %s",
		envelope.DevNest.StartedAt.UTC().Format("2006-01-02 15:04:05 UTC"), envelope.DevNest.Version)
	if envelope.DevNest.DurationMs > 0 {
		fmt.Fprintf(&report, " in %s", duration(float64(envelope.DevNest.DurationMs)))
	}
	report.WriteString("\n")

	if envelope.Error != nil {
		fmt.Fprintf(&report, "\n## Failed\n\n%s\n", envelope.Error.Message)
		if envelope.Error.Hint != "" {
			fmt.Fprintf(&report, "\n%s\n", envelope.Error.Hint)
		}
	}

	if envelope.Data != nil {
		decoded, err := decode(envelope.Data)
		if err != nil {
			return err
		}
		writeValue(&report, decoded, "Summary", 2)
	}

	if len(envelope.Warnings) > 0 {
		report.WriteString("\n## Warnings\n\n")
		for _, warning := range envelope.Warnings {
			line := warning.Message
			if warning.Path != "" {
				line += " (`" + warning.Path + "`)"
			}
			fmt.Fprintf(&report, "- %s\n", line)
		}
	}

	if _, err := io.WriteString(w, report.String()); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write markdown output")
	}
	return nil
}

// field is one member of an object, kept in the order the result declared it.
// A map would be simpler and would shuffle the report on every run.
type field struct {
	name  string
	value any
}

// decode turns a result into ordered values by walking the JSON it produces.
// Going through JSON rather than reflection is what guarantees the report and
// the machine output describe the same fields: anything the JSON renderer
// omits is not here either.
func decode(data any) (any, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "cannot render result as markdown")
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()

	value, err := decodeValue(decoder)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "cannot render result as markdown")
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}

	switch delimiter {
	case '{':
		var fields []field
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			value, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			fields = append(fields, field{name: fmt.Sprint(key), value: value})
		}
		_, err := decoder.Token() // closing brace
		return fields, err

	case '[':
		items := []any{}
		for decoder.More() {
			item, err := decodeValue(decoder)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		_, err := decoder.Token() // closing bracket
		return items, err
	}

	return nil, errors.New(errors.CodeInternal, "unexpected token %v", delimiter)
}

// writeValue renders one value under a heading. Scalars of an object are
// gathered into one summary table; everything with a shape of its own becomes
// a section below it.
func writeValue(report *strings.Builder, value any, heading string, level int) {
	switch typed := value.(type) {
	case []field:
		writeObject(report, typed, heading, level)
	case []any:
		writeArray(report, typed, heading, level)
	default:
		writeHeading(report, heading, level)
		fmt.Fprintf(report, "%s\n", scalar(heading, value))
	}
}

func writeObject(report *strings.Builder, fields []field, heading string, level int) {
	rows := make([][]string, 0, len(fields))
	sections := make([]field, 0, len(fields))

	for _, member := range fields {
		switch typed := member.value.(type) {
		case []field:
			sections = append(sections, member)
		case []any:
			if len(typed) == 0 {
				rows = append(rows, []string{sentence(member.name), "none"})
				continue
			}
			sections = append(sections, member)
		default:
			rows = append(rows, []string{sentence(member.name), scalar(member.name, member.value)})
		}
	}

	if len(rows) > 0 {
		writeHeading(report, heading, level)
		writeMarkdownTable(report, []string{"Metric", "Value"}, rows)
	}
	for _, section := range sections {
		writeValue(report, section.value, sentence(section.name), level)
	}
}

// writeArray renders a list. A list of objects is a table, which is what most
// results are; a list of scalars is a bullet list, because a one-column table
// is a worse bullet list.
func writeArray(report *strings.Builder, items []any, heading string, level int) {
	if len(items) == 0 {
		return
	}

	if first, ok := items[0].([]field); ok {
		// A row can hold a number, a word, or a date. It cannot hold a whole
		// result: "6 field(s)" in a cell is a report that lost the thing it
		// was written to carry, so anything with structure of its own becomes
		// a section instead.
		if nested(first) {
			writeHeading(report, heading, level)
			for index, item := range items {
				fields, ok := item.([]field)
				if !ok {
					continue
				}
				writeValue(report, fields, itemHeading(fields, index), level+1)
			}
			return
		}

		columns := make([]string, 0, len(first))
		keys := make([]string, 0, len(first))
		for _, member := range first {
			columns = append(columns, sentence(member.name))
			keys = append(keys, member.name)
		}

		rows := make([][]string, 0, len(items))
		for _, item := range items {
			fields, ok := item.([]field)
			if !ok {
				continue
			}
			rows = append(rows, row(fields, keys))
		}

		writeHeading(report, heading, level)
		writeMarkdownTable(report, columns, rows)
		return
	}

	writeHeading(report, heading, level)
	for _, item := range items {
		fmt.Fprintf(report, "- %s\n", scalar(heading, item))
	}
}

// nested reports whether an object holds anything that is not a scalar.
func nested(fields []field) bool {
	for _, member := range fields {
		switch typed := member.value.(type) {
		case []field:
			return true
		case []any:
			if len(typed) > 0 {
				return true
			}
		}
	}
	return false
}

// itemHeading names one entry of a list, preferring whatever the entry calls
// itself over its position, because "## env" is findable in a report and
// "## Item 2" is not.
func itemHeading(fields []field, index int) string {
	for _, key := range []string{"command", "name", "rule", "path", "tool"} {
		for _, member := range fields {
			if member.name != key {
				continue
			}
			if text, ok := member.value.(string); ok && text != "" {
				return text
			}
		}
	}
	return fmt.Sprintf("Item %d", index+1)
}

// row pulls the named fields out of one object, so a member missing from an
// entry leaves an empty cell rather than shifting the whole row left.
func row(fields []field, keys []string) []string {
	cells := make([]string, len(keys))
	for index, key := range keys {
		for _, member := range fields {
			if member.name != key {
				continue
			}
			if nested, ok := member.value.([]field); ok {
				cells[index] = fmt.Sprintf("%d field(s)", len(nested))
				break
			}
			if list, ok := member.value.([]any); ok {
				cells[index] = fmt.Sprintf("%d item(s)", len(list))
				break
			}
			cells[index] = scalar(key, member.value)
			break
		}
	}
	return cells
}

func writeHeading(report *strings.Builder, heading string, level int) {
	fmt.Fprintf(report, "\n%s %s\n\n", strings.Repeat("#", level), heading)
}

func writeMarkdownTable(report *strings.Builder, columns []string, rows [][]string) {
	fmt.Fprintf(report, "| %s |\n", strings.Join(columns, " | "))
	fmt.Fprintf(report, "|%s\n", strings.Repeat("---|", len(columns)))
	for _, cells := range rows {
		escaped := make([]string, len(cells))
		for index, cell := range cells {
			escaped[index] = strings.ReplaceAll(cell, "|", "\\|")
		}
		fmt.Fprintf(report, "| %s |\n", strings.Join(escaped, " | "))
	}
}

// scalar formats one value for a person, using the field name to decide what
// the number means.
func scalar(name string, value any) string {
	switch typed := value.(type) {
	case nil:
		return "none"
	case bool:
		if typed {
			return "yes"
		}
		return "no"
	case string:
		return typed
	case json.Number:
		return number(name, typed)
	default:
		return fmt.Sprint(typed)
	}
}

func number(name string, value json.Number) string {
	switch {
	case strings.HasSuffix(name, "Bytes"), strings.HasSuffix(name, "bytes"):
		if size, err := value.Int64(); err == nil {
			return Bytes(size)
		}
	case strings.HasSuffix(name, "Ms"), strings.HasSuffix(name, "ms"):
		if milliseconds, err := value.Float64(); err == nil {
			return duration(milliseconds)
		}
	}

	if whole, err := value.Int64(); err == nil {
		return Count(int(whole))
	}
	return value.String()
}

func duration(milliseconds float64) string {
	switch {
	case milliseconds < 1000:
		return strconv.FormatFloat(milliseconds, 'f', -1, 64) + " ms"
	case milliseconds < 60000:
		return fmt.Sprintf("%.1f s", milliseconds/1000)
	default:
		return (time.Duration(milliseconds) * time.Millisecond).Round(time.Second).String()
	}
}

// sentence turns a camelCase field name into something a person reads:
// "byExtension" becomes "By extension", "totalBytes" becomes "Total bytes".
func sentence(name string) string {
	// The unit suffix is a convention of the JSON field names, and the value
	// beside the label already carries its unit: "Duration ms | 1 ms" reads
	// like a mistake because it is one.
	if trimmed := strings.TrimSuffix(name, "Ms"); trimmed != name && trimmed != "" {
		name = trimmed
	}

	var spaced strings.Builder
	for index, character := range name {
		if unicode.IsUpper(character) && index > 0 {
			spaced.WriteByte(' ')
			spaced.WriteRune(unicode.ToLower(character))
			continue
		}
		spaced.WriteRune(character)
	}

	text := strings.ReplaceAll(spaced.String(), "-", " ")
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
