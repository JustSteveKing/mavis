package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Colours are the 16 ANSI slots rather than hex, so the interface inherits
// whatever theme the terminal is set to instead of fighting it. The same
// palette as taskgo's, so the two read as siblings.
var (
	colAccent = lipgloss.Color("4")
	colDim    = lipgloss.Color("8")
	colUrgent = lipgloss.Color("1")
	colWarn   = lipgloss.Color("3")
	colOK     = lipgloss.Color("2")
	colAgent  = lipgloss.Color("13")

	styleDim     = lipgloss.NewStyle().Foreground(colDim)
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleAccent  = lipgloss.NewStyle().Foreground(colAccent)
	styleUrgent  = lipgloss.NewStyle().Foreground(colUrgent).Bold(true)
	styleWarn    = lipgloss.NewStyle().Foreground(colWarn)
	styleOK      = lipgloss.NewStyle().Foreground(colOK)
	styleAgent   = lipgloss.NewStyle().Foreground(colAgent)
	styleSection = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// The selection bar. Rows under it are rendered plain and styled once,
	// because a colour reset mid-line would punch a hole in the bar.
	styleSelected        = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colAccent).Bold(true)
	styleSelectedBlurred = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colDim)

	styleKey  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHint = lipgloss.NewStyle().Foreground(colDim)
)

// box draws a rounded panel with its title set into the top border, the way
// lazygit and its siblings do. w and h are the outer dimensions.
func box(title string, w, h int, focused bool, content string) string {
	if w < 4 {
		w = 4
	}
	if h < 3 {
		h = 3
	}
	border := lipgloss.NewStyle().Foreground(colDim)
	titleStyle := border
	if focused {
		border = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
		titleStyle = border
	}
	inner := w - 2
	label := ""
	if title != "" {
		label = truncate(" "+title+" ", inner-2)
	}
	fill := max(inner-1-lipgloss.Width(label), 0)
	top := border.Render("╭─") + titleStyle.Render(label) + border.Render(strings.Repeat("─", fill)+"╮")
	side := border.Render("│")
	bottom := border.Render("╰" + strings.Repeat("─", inner) + "╯")

	lines := strings.Split(content, "\n")
	var b strings.Builder
	b.WriteString(top + "\n")
	for i := 0; i < h-2; i++ {
		var line string
		if i < len(lines) {
			line = truncate(lines[i], inner)
		}
		b.WriteString(side + pad(line, inner) + side + "\n")
	}
	b.WriteString(bottom)
	return b.String()
}

// pad and truncate measure rendered cells rather than bytes, so styled text
// and multi-byte characters such as £ do not throw the layout off.
func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
