package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Column describes one column of a table.
type Column struct {
	// Title is the header text.
	Title string
	// Right aligns the column to the right, which is what numbers want.
	Right bool
}

// WriteTable renders aligned columns with a header row.
//
// No box drawing and no separators beyond the header: the output is meant to
// be read in a terminal and pasted into a message, and every extra character
// is one more thing that wraps badly at eighty columns.
func WriteTable(w io.Writer, columns []Column, rows [][]string) error {
	if len(columns) == 0 {
		return nil
	}

	widths := columnWidths(columns, rows)

	header := make([]string, len(columns))
	for index, column := range columns {
		header[index] = column.Title
	}
	if err := writeRow(w, columns, widths, header); err != nil {
		return err
	}

	rule := make([]string, len(columns))
	for index := range columns {
		rule[index] = strings.Repeat("-", widths[index])
	}
	if err := writeRow(w, columns, widths, rule); err != nil {
		return err
	}

	for _, row := range rows {
		if err := writeRow(w, columns, widths, row); err != nil {
			return err
		}
	}
	return nil
}

func columnWidths(columns []Column, rows [][]string) []int {
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = len([]rune(column.Title))
	}
	for _, row := range rows {
		for index, cell := range row {
			if index >= len(widths) {
				break
			}
			if width := len([]rune(cell)); width > widths[index] {
				widths[index] = width
			}
		}
	}
	return widths
}

func writeRow(w io.Writer, columns []Column, widths []int, cells []string) error {
	var line strings.Builder

	for index, column := range columns {
		cell := ""
		if index < len(cells) {
			cell = cells[index]
		}
		padding := widths[index] - len([]rune(cell))
		if padding < 0 {
			padding = 0
		}

		if column.Right {
			line.WriteString(strings.Repeat(" ", padding))
			line.WriteString(cell)
		} else {
			line.WriteString(cell)
			if index < len(columns)-1 {
				line.WriteString(strings.Repeat(" ", padding))
			}
		}
		if index < len(columns)-1 {
			line.WriteString("  ")
		}
	}

	if _, err := fmt.Fprintln(w, strings.TrimRight(line.String(), " ")); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	return nil
}

// Bytes formats a byte count for a person to read.
//
// Structured output always carries the raw integer; this is only ever a
// rendering. Binary units, because that is what a filesystem reports and what
// every other disk tool on the machine shows.
func Bytes(count int64) string {
	const unit = 1024
	if count < unit {
		return strconv.FormatInt(count, 10) + " B"
	}

	value := float64(count)
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	index := -1
	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}

	format := "%.1f %s"
	if value >= 100 {
		format = "%.0f %s"
	}
	return fmt.Sprintf(format, value, units[index])
}

// Count formats a quantity with thousands separators.
func Count(value int) string {
	text := strconv.Itoa(value)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")

	var grouped strings.Builder
	for index, digit := range text {
		if index > 0 && (len(text)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	if negative {
		return "-" + grouped.String()
	}
	return grouped.String()
}
