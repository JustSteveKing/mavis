package tui

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/charmbracelet/lipgloss"
)

const sideWidth = 20

func (m model) View() string {
	if m.width == 0 {
		return "" // the first frame, before the size arrives
	}
	if m.err != nil {
		return styleUrgent.Render("mavis: "+m.err.Error()) + "\n\n" + styleHint.Render("q to quit")
	}
	if m.mode == modeHelp {
		return m.helpView()
	}

	body := max(m.height-2, 8) // the status line and the footer
	listH := max(body*5/9, 5)
	detailH := body - listH
	mainW := max(m.width-sideWidth, 20)

	side := box("1 Views", sideWidth, body, m.focus == focusViews, m.viewsPanel())
	list := box("2 "+m.view.String(), mainW, listH, m.focus == focusList, m.listPanel(listH-2, mainW-2))
	detail := box("", mainW, detailH, false, m.detailPanel())
	main := lipgloss.JoinVertical(lipgloss.Left, list, detail)
	// Truncated, not wrapped: a footer that wraps on a narrow terminal pushes
	// the whole layout down a line.
	return lipgloss.JoinHorizontal(lipgloss.Top, side, main) + "\n" +
		truncate(m.statusLine(), m.width) + "\n" + truncate(m.footer(), m.width)
}

func (m model) viewsPanel() string {
	counts := [viewCount]int{
		len(m.today.Unpaid) + len(m.today.Overdue) + len(m.today.Retainers) + len(m.today.Quotes),
		len(m.clients), len(m.invoices), len(m.quotes),
	}
	var lines []string
	for v := viewKind(0); v < viewCount; v++ {
		marker := " "
		if v == m.view {
			marker = "›"
		}
		label := fmt.Sprintf("%s%-10s %3d ", marker, v, counts[v])
		switch {
		case v == m.view && m.focus == focusViews:
			label = styleSelected.Render(pad(label, sideWidth-2))
		case v == m.view:
			label = styleSelectedBlurred.Render(pad(label, sideWidth-2))
		}
		lines = append(lines, label)
	}
	if m.problems > 0 {
		lines = append(lines, "", styleWarn.Render(fmt.Sprintf(" %d file(s) unread", m.problems)), styleHint.Render(" mavis client list"), styleHint.Render(" names them"))
	}
	return strings.Join(lines, "\n")
}

// listPanel renders the rows that fit, scrolled so the cursor stays in view.
func (m model) listPanel(height, width int) string {
	if len(m.rows) == 0 {
		return styleHint.Render(" Nothing here yet.")
	}
	cursor := m.cursor[m.view]
	start := 0
	if cursor >= height {
		start = cursor - height + 1
	}
	var lines []string
	for i := start; i < len(m.rows) && i < start+height; i++ {
		r := m.rows[i]
		if r.header {
			lines = append(lines, styleSection.Render(" "+r.text))
			continue
		}
		// The marker as well as the bar: the bar is colour, and the cursor
		// has to show without it.
		text := "   " + r.text
		if i == cursor {
			text = " › " + r.text
		}
		switch {
		case i == cursor && m.focus == focusList:
			text = styleSelected.Render(pad(text, width))
		case i == cursor:
			text = styleSelectedBlurred.Render(pad(text, width))
		default:
			text = tone(r.tone).Render(text)
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func tone(t string) lipgloss.Style {
	switch t {
	case "urgent":
		return styleUrgent
	case "warn":
		return styleWarn
	case "ok":
		return styleOK
	case "dim":
		return styleDim
	case "agent":
		return styleAgent
	}
	return lipgloss.NewStyle()
}

// ---------------------------------------------------------------- detail

func (m model) detailPanel() string {
	if m.detailNote != "" {
		return indent(m.detailNote)
	}
	r := m.selected()
	if r == nil {
		return ""
	}
	switch {
	case r.invoice != "":
		if inv, ok := m.invoiceFor(r); ok {
			return indent(m.invoiceDetail(inv))
		}
	case r.quote != "":
		for _, q := range m.quotes {
			if q.Number == r.quote || q.Slug == r.quote {
				return indent(m.quoteDetail(q))
			}
		}
	case r.client != "":
		for _, c := range m.clients {
			if c.Slug == r.client {
				return indent(m.clientDetail(c))
			}
		}
	}
	return ""
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = " " + l
	}
	return strings.Join(lines, "\n")
}

func (m model) clientDetail(c store.Client) string {
	var b strings.Builder
	b.WriteString(styleBold.Render(c.Name) + styleDim.Render("  "+c.Slug+" · "+c.Status))
	if c.StatusSince != "" {
		b.WriteString(styleDim.Render(" since " + c.StatusSince))
	}
	b.WriteString("\n")
	if who := strings.TrimSpace(strings.Join([]string{c.Contact, c.Email, c.Phone}, "  ")); who != "" {
		b.WriteString(who + "\n")
	}

	var active []string
	for _, e := range m.engagements {
		if e.Client == c.Slug && e.Status != "done" {
			detail := e.Status
			if e.Basis != "" {
				detail += ", " + e.Basis
			}
			active = append(active, e.Title+styleDim.Render(" ("+detail+")"))
		}
	}
	if len(active) > 0 {
		b.WriteString("\n" + styleSection.Render("Engagements") + "\n  " + strings.Join(active, "\n  ") + "\n")
	}

	var open, recent []string
	for i := len(m.logs) - 1; i >= 0; i-- {
		l := m.logs[i]
		if l.Client != c.Slug {
			continue
		}
		for _, f := range l.FollowUps {
			if !f.Done {
				due := ""
				if f.Due != "" {
					due = styleDim.Render(" due " + f.Due)
				}
				open = append(open, "[ ] "+f.Text+due)
			}
		}
		if len(recent) < 4 {
			line := styleDim.Render(l.Date[:10]+" "+l.Kind+"  ") + firstLine(l.Summary)
			recent = append(recent, line)
		}
	}
	if len(open) > 0 {
		b.WriteString("\n" + styleSection.Render("Follow-ups") + "\n  " + strings.Join(open, "\n  ") + "\n")
	}
	if len(recent) > 0 {
		b.WriteString("\n" + styleSection.Render("Recent") + "\n  " + strings.Join(recent, "\n  ") + "\n")
	}
	return b.String()
}

func (m model) invoiceDetail(inv store.Invoice) string {
	var b strings.Builder
	name := inv.Number
	if name == "" {
		name = "Draft " + inv.Slug
	}
	if inv.Kind == "credit" {
		name = "Credit note " + strings.TrimPrefix(name, "Draft ") + " against " + inv.Credits
	}
	b.WriteString(styleBold.Render(name) + styleDim.Render("  "+inv.Client+" · "+inv.Display()) + "\n")
	var facts []string
	for _, f := range [][2]string{{"issued", inv.Issued}, {"due", inv.Due}, {"paid", inv.Paid}} {
		if f[1] != "" {
			facts = append(facts, f[0]+" "+f[1])
		}
	}
	if len(facts) > 0 {
		b.WriteString(styleDim.Render(strings.Join(facts, " · ")) + "\n")
	}
	b.WriteString("\n")
	for _, l := range inv.Lines {
		b.WriteString(fmt.Sprintf("%-38s %10s %12s\n", truncate(l.Description, 38), l.Quantity(), l.Amount.Display()))
	}
	b.WriteString(fmt.Sprintf("%-38s %10s %12s\n", "", "Net", inv.Net.Display()))
	if inv.VAT > 0 {
		b.WriteString(fmt.Sprintf("%-38s %10s %12s\n", "", "VAT", inv.VAT.Display()))
	}
	b.WriteString(styleBold.Render(fmt.Sprintf("%-38s %10s %12s", "", "Total", gbp(inv.Total, inv.Currency))) + "\n")
	if inv.Credited > 0 {
		b.WriteString(styleDim.Render(fmt.Sprintf("Credited %s, %s still owed", inv.Credited.Display(), inv.Balance.Display())) + "\n")
	}
	if inv.Balance > 0 && inv.Due < m.today.Date {
		sent, _ := m.s.Reminders(inv.Number)
		chase := "Not chased yet."
		if len(sent) > 0 {
			chase = fmt.Sprintf("%d reminder(s) sent, last %s.", len(sent), sent[len(sent)-1].Date[:10])
		}
		b.WriteString("\n" + styleUrgent.Render("Overdue. ") + chase + styleHint.Render(" r previews the next reminder.") + "\n")
	}
	return b.String()
}

func (m model) quoteDetail(q store.Quote) string {
	var b strings.Builder
	name := q.Number
	if name == "" {
		name = "Draft " + q.Slug
	}
	b.WriteString(styleBold.Render(name+": "+q.Title) + styleDim.Render("  "+q.Client+" · "+q.Display(m.today.Date)) + "\n")
	if q.Sent != "" {
		b.WriteString(styleDim.Render("sent "+q.Sent+" · valid until "+q.ValidUntil) + "\n")
	}
	if q.Scope != "" {
		b.WriteString("\n" + q.Scope + "\n")
	}
	b.WriteString("\n")
	for _, l := range q.Lines {
		b.WriteString(fmt.Sprintf("%-38s %10s %12s\n", truncate(l.Description, 38), l.Quantity(), l.Amount.Display()))
	}
	b.WriteString(styleBold.Render(fmt.Sprintf("%-38s %10s %12s", "", "Net", gbp(q.Net, q.Currency))) + "\n")
	return b.String()
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// ---------------------------------------------------------------- chrome

func (m model) statusLine() string {
	switch m.mode {
	case modeNote:
		return m.input.View()
	case modeConfirmPaid:
		inv, _ := m.invoiceFor(m.selected())
		return styleWarn.Render(fmt.Sprintf("Mark %s paid today, %s? y/n", inv.Number, gbp(inv.Balance, inv.Currency)))
	}
	if m.status != "" {
		return styleAccent.Render(m.status)
	}
	return styleHint.Render(m.today.Date)
}

// footer offers only the keys that do something for what is selected.
func (m model) footer() string {
	keys := [][2]string{{"tab", "panel"}, {"j/k", "move"}}
	if m.focus == focusList {
		if r := m.selected(); r != nil {
			if m.view == viewToday {
				keys = append(keys, [2]string{"enter", "open"})
			}
			if r.followUp != nil {
				keys = append(keys, [2]string{"x", "done"})
			}
			if r.client != "" {
				keys = append(keys, [2]string{"n", "note"})
			}
			if inv, ok := m.invoiceFor(r); ok && inv.Kind == "invoice" && inv.Status == "issued" {
				keys = append(keys, [2]string{"p", "paid"})
				if inv.Balance > 0 && inv.Due < m.today.Date {
					keys = append(keys, [2]string{"r", "reminder"})
				}
			}
		}
	}
	keys = append(keys, [2]string{"?", "help"}, [2]string{"q", "quit"})
	var parts []string
	for _, k := range keys {
		parts = append(parts, styleKey.Render(k[0])+" "+styleHint.Render(k[1]))
	}
	return strings.Join(parts, "  ")
}

func (m model) helpView() string {
	rows := [][2]string{
		{"tab, 1, 2", "move between the Views panel and the list"},
		{"j, k", "move down and up"},
		{"enter", "in Today, open the invoice, quote or client behind a row"},
		{"x", "tick off the selected follow-up"},
		{"n", "add a note to the selected client"},
		{"p", "mark the selected invoice paid today, after asking"},
		{"r", "preview the next payment reminder for an overdue invoice"},
		{"R", "reload now (it also reloads every two seconds)"},
		{"q", "quit"},
	}
	var b strings.Builder
	b.WriteString(styleBold.Render("mavis") + "\n\n")
	for _, r := range rows {
		b.WriteString("  " + styleKey.Render(pad(r[0], 11)) + r[1] + "\n")
	}
	b.WriteString("\n  " + styleHint.Render("Issuing, sending and recording reminders stay at the CLI, where they are deliberate.") + "\n")
	b.WriteString("  " + styleHint.Render("Changes an agent makes over MCP appear here on their own.") + "\n\n")
	b.WriteString("  " + styleHint.Render("Any key to go back."))
	return b.String()
}
