package output

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const defaultTableWidth = 120

// RenderTable writes a compact, display-width-aware table for human output.
// JSON output should use explicit DTOs instead of this renderer.
func RenderTable(w io.Writer, headers []string, rows [][]string) error {
	if len(headers) == 0 {
		return nil
	}

	columns := len(headers)
	widths := make([]int, columns)
	minimums := make([]int, columns)
	for i, header := range headers {
		widths[i] = displayWidth(header)
		minimums[i] = minimumColumnWidth(header)
	}

	normalizedRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, columns)
		for i := range columns {
			if i < len(row) {
				cells[i] = normalizeCell(row[i])
			}
			widths[i] = maxInt(widths[i], displayWidth(cells[i]))
		}
		normalizedRows = append(normalizedRows, cells)
	}

	gap := 2
	maxWidth := tableWidth(w)
	// Keep the normal two-space separation, but give a narrow terminal one
	// space between columns before shortening otherwise readable headers.
	if tableWidthFor(minimums, gap) > maxWidth && tableWidthFor(minimums, 1) <= maxWidth {
		gap = 1
	}
	for tableWidthFor(widths, gap) > maxWidth {
		index := shrinkableColumn(widths, minimums)
		if index < 0 {
			index = anyShrinkableColumn(widths)
			if index < 0 {
				break
			}
		}
		widths[index]--
	}

	if err := writeTableRow(w, headers, widths, gap); err != nil {
		return err
	}
	for _, row := range normalizedRows {
		if err := writeTableRow(w, row, widths, gap); err != nil {
			return err
		}
	}
	return nil
}

func writeTableRow(w io.Writer, cells []string, widths []int, gap int) error {
	var b strings.Builder
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = fitCell(cells[i], width)
		}
		b.WriteString(cell)
		if i < len(widths)-1 {
			b.WriteString(strings.Repeat(" ", maxInt(0, width-displayWidth(cell))))
			b.WriteString(strings.Repeat(" ", gap))
		}
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}

func shrinkableColumn(widths, minimums []int) int {
	index := -1
	for i := range widths {
		if widths[i] <= minimums[i] {
			continue
		}
		if index < 0 || widths[i] > widths[index] || (widths[i] == widths[index] && i > index) {
			index = i
		}
	}
	return index
}

func anyShrinkableColumn(widths []int) int {
	index := -1
	for i, width := range widths {
		if width <= 1 {
			continue
		}
		if index < 0 || width > widths[index] || (width == widths[index] && i > index) {
			index = i
		}
	}
	return index
}

func minimumColumnWidth(header string) int {
	minimum := displayWidth(header)
	switch strings.ToLower(header) {
	case "name":
		minimum = maxInt(minimum, 12)
	case "template":
		minimum = maxInt(minimum, 8)
	case "status":
		minimum = maxInt(minimum, 6)
	case "model":
		minimum = maxInt(minimum, 7)
	case "harness":
		minimum = maxInt(minimum, 10)
	case "cwd":
		minimum = maxInt(minimum, 12)
	case "created", "ended":
		minimum = maxInt(minimum, 11)
	case "last activity":
		minimum = maxInt(minimum, 13)
	case "reason":
		minimum = maxInt(minimum, 10)
	case "description", "detail":
		minimum = maxInt(minimum, 20)
	case "check":
		minimum = maxInt(minimum, 12)
	case "instance":
		minimum = maxInt(minimum, 12)
	case "state":
		minimum = maxInt(minimum, 8)
	case "elapsed":
		minimum = maxInt(minimum, 8)
	}
	return minimum
}

func tableWidthFor(widths []int, gap int) int {
	total := maxInt(0, len(widths)-1) * gap
	for _, width := range widths {
		total += width
	}
	return total
}

func tableWidth(w io.Writer) int {
	if raw := strings.TrimSpace(os.Getenv("COLUMNS")); raw != "" {
		if width, err := strconv.Atoi(raw); err == nil && width > 0 {
			return width
		}
	}
	if file, ok := w.(*os.File); ok && file != nil {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			if width := terminalWidthFromFile(file); width > 0 {
				return width
			}
		}
	}
	return defaultTableWidth
}

func normalizeCell(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func fitCell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}

	limit := width - displayWidth("…")
	var b strings.Builder
	used := 0
	for _, r := range s {
		runeWidth := runeDisplayWidth(r)
		if used+runeWidth > limit {
			break
		}
		b.WriteRune(r)
		used += runeWidth
	}
	b.WriteString("…")
	return b.String()
}

func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if unicode.IsControl(r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
