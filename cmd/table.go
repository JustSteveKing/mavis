package cmd

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// table writes rows in columns, text left-aligned and the columns listed in
// right right-aligned, so figures line up on their last digit. tabwriter
// aligns a whole table one way or the other, which reads badly when names
// and amounts share a row.
func table(w io.Writer, indent string, right map[int]bool, rows [][]string) {
	widths := map[int]int{}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	for _, row := range rows {
		var b strings.Builder
		b.WriteString(indent)
		for i, cell := range row {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell))
			if i > 0 {
				b.WriteString("  ")
			}
			if right[i] {
				b.WriteString(pad + cell)
			} else if i < len(row)-1 {
				b.WriteString(cell + pad)
			} else {
				b.WriteString(cell)
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}
