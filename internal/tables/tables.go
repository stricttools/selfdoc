// Package tables renders data as Markdown tables with per-column alignment,
// optional pretty-printing, and pipe escaping that leaves inline code alone.
package tables

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/stricttools/selfdoc/internal/util"
)

// EscapePipes escapes pipe characters in text, preserving pipes inside
// backtick spans.
//
// A pipe outside a backtick span would end the table cell, so it becomes
// "\|"; a pipe inside one is part of an inline code literal and stays. If a
// backtick opens a span but never closes, the pipes after that backtick are
// escaped as if the span had never opened -- an unbalanced backtick renders as
// a literal backtick, so its pipes are cell-ending pipes again.
func EscapePipes(text string) string {
	// result holds one entry per source character, so an entry can be the
	// two-character escape while still corresponding to one input rune --
	// which is what lets the unclosed-backtick rescan below address the
	// backtick's own position.
	var result []string
	inBacktick := false
	backtickStart := 0
	for _, ch := range text {
		switch {
		case ch == '`':
			if !inBacktick {
				inBacktick = true
				backtickStart = len(result)
			} else {
				inBacktick = false
				backtickStart = 0
			}
			result = append(result, "`")
		case ch == '|' && !inBacktick:
			result = append(result, `\|`)
		default:
			result = append(result, string(ch))
		}
	}

	if inBacktick {
		// Unclosed backtick: re-scan from the backtick position and escape
		// the pipes the span was protecting.
		for i := backtickStart; i < len(result); i++ {
			if result[i] == "|" {
				result[i] = `\|`
			}
		}
	}

	return strings.Join(result, "")
}

// RenderMarkdownTable renders a Markdown table from headers and rows.
//
// align gives the per-column alignment ("left", "center" or "right"); it may
// be nil, shorter than headers (the remaining columns get no alignment marker)
// or longer (the extra entries are ignored, though every entry is still
// validated). pretty pads every cell so the rendered source lines up in a text
// editor.
//
// A row with fewer cells than there are headers is padded with empty cells; a
// row with more is an error. It also errors on empty headers, on an alignment
// value that is none of the three, and on a newline inside any cell or header.
func RenderMarkdownTable(headers []string, rows [][]string, align []string, pretty bool) (string, error) {
	if len(headers) == 0 {
		return "", fmt.Errorf("headers must not be empty")
	}

	numCols := len(headers)

	// Validate align.
	for _, a := range align {
		if a != "left" && a != "center" && a != "right" {
			return "", fmt.Errorf(
				"invalid alignment %s, must be one of: left, center, right",
				util.PythonRepr(a),
			)
		}
	}

	// Validate and normalize rows.
	processedRows := make([][]string, 0, len(rows))
	for rowIdx, row := range rows {
		if len(row) > numCols {
			return "", fmt.Errorf(
				"row %d has %d cells, but only %d headers",
				rowIdx, len(row), numCols,
			)
		}
		normalized := make([]string, 0, numCols)
		for _, cell := range row {
			if strings.Contains(cell, "\n") {
				return "", fmt.Errorf("cell values must not contain newline characters")
			}
			normalized = append(normalized, EscapePipes(cell))
		}
		for len(normalized) < numCols {
			normalized = append(normalized, "")
		}
		processedRows = append(processedRows, normalized)
	}

	// Escape pipes in headers too.
	escapedHeaders := make([]string, 0, numCols)
	for _, h := range headers {
		if strings.Contains(h, "\n") {
			return "", fmt.Errorf("cell values must not contain newline characters")
		}
		escapedHeaders = append(escapedHeaders, EscapePipes(h))
	}

	sepCells := make([]string, numCols)
	for i := range sepCells {
		sepCells[i] = sepCell(align, i)
	}

	var headerLine, sepLine string
	var dataLines []string
	if pretty {
		// Column width is the widest of the header, the separator and every
		// row cell, measured in characters rather than bytes so a non-ASCII
		// cell pads the way Python's len() and str.ljust() did.
		colWidths := make([]int, numCols)
		for col := 0; col < numCols; col++ {
			w := utf8.RuneCountInString(escapedHeaders[col])
			if n := utf8.RuneCountInString(sepCells[col]); n > w {
				w = n
			}
			for _, row := range processedRows {
				if n := utf8.RuneCountInString(row[col]); n > w {
					w = n
				}
			}
			colWidths[col] = w
		}

		pad := func(text string, col int) string {
			if n := utf8.RuneCountInString(text); n < colWidths[col] {
				return text + strings.Repeat(" ", colWidths[col]-n)
			}
			return text
		}

		headerLine = joinCells(escapedHeaders, pad)
		padded := make([]string, numCols)
		for c := 0; c < numCols; c++ {
			padded[c] = padSep(sepCells[c], colWidths[c])
		}
		sepLine = joinCells(padded, nil)
		for _, row := range processedRows {
			dataLines = append(dataLines, joinCells(row, pad))
		}
	} else {
		headerLine = joinCells(escapedHeaders, nil)
		sepLine = joinCells(sepCells, nil)
		for _, row := range processedRows {
			dataLines = append(dataLines, joinCells(row, nil))
		}
	}

	lines := append([]string{headerLine, sepLine}, dataLines...)
	return strings.Join(lines, "\n"), nil
}

// joinCells renders one table row. pad, when non-nil, is applied to each cell
// with its column index.
func joinCells(cells []string, pad func(string, int) string) string {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		if pad != nil {
			parts[i] = pad(cell, i)
		} else {
			parts[i] = cell
		}
	}
	return "| " + strings.Join(parts, " | ") + " |"
}

// sepCell is the separator cell for one column, carrying the column's
// alignment marker when align declares one.
func sepCell(align []string, colIdx int) string {
	if colIdx < len(align) {
		switch align[colIdx] {
		case "left":
			return ":---"
		case "center":
			return ":---:"
		case "right":
			return "---:"
		}
	}
	return "---"
}

// padSep extends a separator cell's dashes to fill width, preserving the colon
// markers that carry the alignment.
func padSep(text string, width int) string {
	switch {
	case strings.HasPrefix(text, ":") && strings.HasSuffix(text, ":"):
		return ":" + dashes(width-2) + ":"
	case strings.HasPrefix(text, ":"):
		return ":" + dashes(width-1)
	case strings.HasSuffix(text, ":"):
		return dashes(width-1) + ":"
	default:
		return dashes(width)
	}
}

// dashes is Python's "-" * n, which yields the empty string for a
// non-positive count where [strings.Repeat] would panic.
func dashes(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("-", n)
}
