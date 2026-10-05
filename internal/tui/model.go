// Package tui is mavis's interactive terminal interface.
//
// The layout follows taskgo's, and the lazygit family's: a numbered Views
// panel on the left, the chosen view's list on the right with a detail pane
// under it that follows the cursor, and a footer whose keys change with what
// is selected.
//
// It reloads from the store on a timer as well as after its own edits, so a
// call an agent logs over MCP appears without anyone doing anything. The TUI
// is a view onto the files, not a cache of them. It never reloads while you
// are typing or confirming: refreshing under the cursor is worse than being
// two seconds stale.
//
// It acts as the human, so it may mark an invoice paid where an agent may
// not. Anything that cannot be taken back asks first.
package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

const (
	refreshEvery = 2 * time.Second
	statusHold   = 4 * time.Second
)

// Options are the parts of the config the TUI needs.
type Options struct {
	Quiet     store.Quiet
	Signoff   string
	Statutory bool
}

type viewKind int

const (
	viewToday viewKind = iota
	viewClients
	viewInvoices
	viewQuotes
	viewCount
)

func (v viewKind) String() string {
	return [...]string{"Today", "Clients", "Invoices", "Quotes"}[v]
}

type focus int

const (
	focusViews focus = iota
	focusList
)

type mode int

const (
	modeNormal mode = iota
	modeForm        // a huh form is open in the detail pane
	modeHelp
)

// row is one line in the list. Headers divide a view into sections and are
// never selectable; the cursor steps over them.
//
// A row is cells, not a preformatted line, so the list can align columns
// across the whole view. text is the cells joined, for matching a row
// across reloads.
type row struct {
	header bool
	cells  []string
	text   string
	count  int    // on a header: how many rows the section holds
	tone   string // "", "urgent", "warn", "ok", "dim"

	client     string
	engagement string
	invoice    string
	quote      string
	followUp   *store.FollowUp
}

func item(tone string, cells ...string) row {
	return row{cells: cells, text: strings.Join(cells, "  "), tone: tone}
}

func (r row) selectable() bool { return !r.header }

type model struct {
	s *store.Store
	o Options

	width, height int
	focus         focus
	view          viewKind
	cursor        [viewCount]int
	rows          []row
	mode          mode
	status        string
	err           error

	// detailNote replaces the detail pane until the cursor moves: a reminder
	// preview, say.
	detailNote string

	form       *huh.Form
	formTitle  string
	formSubmit func() (outcome, error)

	// The store, as of the last load.
	today       store.Today
	clients     []store.Client
	engagements []store.Engagement
	logs        []store.LogEntry
	invoices    []store.Invoice
	quotes      []store.Quote
	problems    int

	monthMinutes int // time logged this month, for the summary
}

// New returns the model for a store.
func New(s *store.Store, o Options) tea.Model {
	m := model{s: s, o: o, focus: focusList}
	m.reload()
	return m
}

type tickMsg time.Time
type clearStatusMsg struct{}

func (m model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func clearStatus() tea.Cmd {
	return tea.Tick(statusHold, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

// reload reads everything again and rebuilds the rows, keeping the cursor
// on the same thing where it still exists.
func (m *model) reload() {
	before := m.selected()
	var problems []store.Problem
	var p []store.Problem
	var err error
	if m.today, p, err = m.s.Today(m.o.Quiet); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	if m.clients, p, err = m.s.Clients(); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	if m.engagements, p, err = m.s.Engagements(); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	if m.logs, p, err = m.s.Logs(); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	if m.invoices, p, err = m.s.Invoices(); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	if m.quotes, p, err = m.s.Quotes(); err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	entries, p, err := m.s.TimeEntries()
	if err != nil {
		m.err = err
		return
	}
	problems = append(problems, p...)
	m.monthMinutes = 0
	for _, e := range entries {
		if strings.HasPrefix(e.Date, m.today.Date[:7]) {
			m.monthMinutes += e.Minutes
		}
	}
	m.problems = len(problems)
	m.err = nil
	m.buildRows()
	m.restore(before)
}

func (m *model) buildRows() {
	switch m.view {
	case viewToday:
		m.rows = m.todayRows()
	case viewClients:
		m.rows = m.clientRows()
	case viewInvoices:
		m.rows = m.invoiceRows()
	case viewQuotes:
		m.rows = m.quoteRows()
	}
	m.clamp()
}

func (m model) todayRows() []row {
	t := m.today
	var rows []row
	section := func(title string, n int) { rows = append(rows, row{header: true, text: title, count: n}) }
	day := t.Date

	if len(t.Unpaid) > 0 {
		section("Overdue invoices", len(t.Unpaid))
		for _, inv := range t.Unpaid {
			r := item("urgent", inv.Number, inv.Client, gbp(inv.Balance, inv.Currency)+" owed", "due "+rel(inv.Due, day))
			r.client, r.invoice = inv.Client, inv.Number
			rows = append(rows, r)
		}
	}
	if len(t.Overdue) > 0 {
		section("Overdue follow-ups", len(t.Overdue))
		for i := range t.Overdue {
			f := t.Overdue[i]
			r := item("urgent", f.Text, f.Client, "", "due "+rel(f.Due, day))
			r.client, r.followUp = f.Client, &f
			rows = append(rows, r)
		}
	}
	if len(t.Retainers) > 0 {
		section("Retainers to bill", len(t.Retainers))
		for _, rd := range t.Retainers {
			r := item("warn", rd.Title, rd.Client, rd.Rate, monthName(rd.Month))
			r.client, r.engagement = rd.Client, rd.Engagement
			rows = append(rows, r)
		}
	}
	if len(t.Quotes) > 0 {
		section("Quotes waiting", len(t.Quotes))
		for _, q := range t.Quotes {
			tone, when := "", "sent "+rel(q.Sent, day)
			if q.Expired(day) {
				tone, when = "warn", "expired "+rel(q.ValidUntil, day)
			}
			r := item(tone, q.Number, q.Client, q.Title, when)
			r.client, r.quote = q.Client, q.Number
			rows = append(rows, r)
		}
	}
	if len(t.ThisWeek) > 0 {
		section("Due this week", len(t.ThisWeek))
		for i := range t.ThisWeek {
			f := t.ThisWeek[i]
			r := item("", f.Text, f.Client, "", "due "+rel(f.Due, day))
			r.client, r.followUp = f.Client, &f
			rows = append(rows, r)
		}
	}
	if len(t.Moves) > 0 {
		section("Worth a move?", len(t.Moves))
		for _, n := range t.Moves {
			r := item("dim", n.Status+" to "+n.Suggest+"?", n.Client, "", fmt.Sprintf("quiet %d days", n.QuietDays))
			r.client = n.Client
			rows = append(rows, r)
		}
	}
	if len(t.KeepInTouch) > 0 {
		section("Keep in touch", len(t.KeepInTouch))
		for _, n := range t.KeepInTouch {
			last := "no contact logged"
			if n.LastContact != "" {
				last = "last spoke " + rel(n.LastContact, day)
			}
			r := item("dim", n.Name, n.Client, "", last)
			r.client = n.Client
			rows = append(rows, r)
		}
	}
	if len(t.Engagements) > 0 {
		section("Active engagements", len(t.Engagements))
		for _, e := range t.Engagements {
			basis := e.Basis
			if e.Rate != "" {
				basis += " @ " + e.Rate
			}
			r := item("dim", e.Title, e.Client, basis, "")
			r.client, r.engagement = e.Client, e.Slug
			rows = append(rows, r)
		}
	}
	return rows
}

func (m model) clientRows() []row {
	last := map[string]string{}
	for _, l := range m.logs {
		if d := l.Date[:10]; d > last[l.Client] {
			last[l.Client] = d
		}
	}
	var rows []row
	// Active first: they are the ones opened most.
	for _, status := range []string{"active", "warm", "prospect", "cold"} {
		var in []store.Client
		for _, c := range m.clients {
			if c.Status == status {
				in = append(in, c)
			}
		}
		if len(in) == 0 {
			continue
		}
		rows = append(rows, row{header: true, text: strings.ToUpper(status[:1]) + status[1:], count: len(in)})
		for _, c := range in {
			tone := ""
			if status == "cold" {
				tone = "dim"
			}
			spoke := "no contact logged"
			if d := last[c.Slug]; d != "" {
				spoke = "last spoke " + rel(d, m.today.Date)
			}
			r := item(tone, c.Name, c.Slug, c.Contact, spoke)
			r.client = c.Slug
			rows = append(rows, r)
		}
	}
	return rows
}

func (m model) invoiceRows() []row {
	var rows []row
	for i := len(m.invoices) - 1; i >= 0; i-- { // newest first
		inv := m.invoices[i]
		name := inv.Number
		if name == "" {
			name = inv.Slug
		}
		tone, when := "", ""
		switch {
		case inv.Status == "draft":
			tone = "dim"
		case inv.Balance > 0 && inv.Due < m.today.Date:
			tone, when = "urgent", "due "+rel(inv.Due, m.today.Date)
		case inv.Balance > 0:
			when = "due " + rel(inv.Due, m.today.Date)
		case inv.Status == "paid":
			tone, when = "ok", "paid "+rel(inv.Paid, m.today.Date)
		case inv.FullyCredited():
			tone = "ok"
		}
		amount := gbp(inv.Total, inv.Currency)
		if inv.Kind == "credit" {
			amount = "-" + amount
		}
		r := item(tone, name, inv.Display(), inv.Client, amount, when)
		r.client, r.invoice = inv.Client, name
		rows = append(rows, r)
	}
	return rows
}

func (m model) quoteRows() []row {
	var rows []row
	for i := len(m.quotes) - 1; i >= 0; i-- {
		q := m.quotes[i]
		name := q.Number
		if name == "" {
			name = q.Slug
		}
		tone := ""
		switch q.Display(m.today.Date) {
		case "draft", "declined":
			tone = "dim"
		case "expired":
			tone = "warn"
		case "accepted":
			tone = "ok"
		}
		r := item(tone, name, q.Display(m.today.Date), q.Client, q.Title)
		r.client, r.quote = q.Client, name
		rows = append(rows, r)
	}
	return rows
}

// ---------------------------------------------------------------- cursor

func (m model) selected() *row {
	c := m.cursor[m.view]
	if c < 0 || c >= len(m.rows) || !m.rows[c].selectable() {
		return nil
	}
	r := m.rows[c]
	return &r
}

// clamp keeps the cursor on a selectable row.
func (m *model) clamp() {
	c := min(max(m.cursor[m.view], 0), len(m.rows)-1)
	for c >= 0 && c < len(m.rows) && !m.rows[c].selectable() {
		c++
	}
	if c >= len(m.rows) {
		c = len(m.rows) - 1
		for c >= 0 && !m.rows[c].selectable() {
			c--
		}
	}
	m.cursor[m.view] = max(c, 0)
}

// move steps the cursor by d selectable rows.
func (m *model) move(d int) {
	c := m.cursor[m.view]
	for {
		next := c + d
		if next < 0 || next >= len(m.rows) {
			return
		}
		c = next
		if m.rows[c].selectable() {
			m.cursor[m.view] = c
			m.detailNote = ""
			return
		}
	}
}

// restore puts the cursor back on the row that was selected before a
// reload, matching on what the row is about rather than its position.
func (m *model) restore(before *row) {
	if before == nil {
		return
	}
	i := slices.IndexFunc(m.rows, func(r row) bool {
		return r.selectable() && r.text == before.text
	})
	if i >= 0 {
		m.cursor[m.view] = i
	}
}

// ---------------------------------------------------------------- helpers

func gbp(p money.Pence, currency string) string {
	switch currency {
	case "GBP":
		return "£" + p.Display()
	case "EUR":
		return "€" + p.Display()
	case "USD":
		return "$" + p.Display()
	}
	return p.Display() + " " + currency
}

// rel says when a date is, from today, the way you would say it: "today",
// "in 3 days", "12 days ago", and the date itself once it is far enough
// off that a count stops helping.
func rel(date, today string) string {
	d, err1 := time.Parse("2006-01-02", date)
	t, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return date
	}
	n := int(d.Sub(t).Hours() / 24)
	switch {
	case n == 0:
		return "today"
	case n == 1:
		return "tomorrow"
	case n == -1:
		return "yesterday"
	case n > 1 && n <= 60:
		return fmt.Sprintf("in %d days", n)
	case n < -1 && n >= -60:
		return fmt.Sprintf("%d days ago", -n)
	case d.Year() == t.Year():
		return "on " + d.Format("2 Jan")
	}
	return "on " + d.Format("2 Jan 2006")
}

func monthName(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return t.Format("January 2006")
}

// selectWhere moves the cursor to the first row matching.
func (m *model) selectWhere(match func(row) bool) {
	for i, r := range m.rows {
		if r.selectable() && match(r) {
			m.cursor[m.view] = i
			return
		}
	}
}

// clientBySlug finds a client among those loaded.
func (m model) clientBySlug(slug string) (store.Client, bool) {
	for _, c := range m.clients {
		if c.Slug == slug {
			return c, true
		}
	}
	return store.Client{}, false
}
