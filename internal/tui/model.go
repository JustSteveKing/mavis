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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	modeNormal      mode = iota
	modeNote             // typing a note against a client
	modeConfirmPaid      // y/n before marking an invoice paid
	modeHelp
)

// row is one line in the list. Headers divide Today into sections and are
// never selectable; the cursor steps over them.
type row struct {
	header bool
	text   string
	tone   string // "", "urgent", "warn", "ok", "dim", "agent"

	client   string
	invoice  string
	quote    string
	followUp *store.FollowUp
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
	input         textinput.Model
	status        string
	err           error

	// detailNote replaces the detail pane until the cursor moves: a reminder
	// preview, say.
	detailNote string

	// The store, as of the last load.
	today       store.Today
	clients     []store.Client
	engagements []store.Engagement
	logs        []store.LogEntry
	invoices    []store.Invoice
	quotes      []store.Quote
	problems    int
}

// New returns the model for a store.
func New(s *store.Store, o Options) tea.Model {
	in := textinput.New()
	in.Prompt = "note> "
	in.CharLimit = 500
	m := model{s: s, o: o, input: in, focus: focusList}
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
	section := func(title string) { rows = append(rows, row{header: true, text: title}) }

	if len(t.Unpaid) > 0 {
		section("Overdue invoices")
		for _, inv := range t.Unpaid {
			rows = append(rows, row{
				text:    fmt.Sprintf("%s  %s  %s owed, due %s", inv.Number, inv.Client, gbp(inv.Balance, inv.Currency), inv.Due),
				tone:    "urgent",
				client:  inv.Client,
				invoice: inv.Number,
			})
		}
	}
	if len(t.Overdue) > 0 {
		section("Overdue follow-ups")
		for i := range t.Overdue {
			f := t.Overdue[i]
			rows = append(rows, row{text: fmt.Sprintf("%s  %s, due %s", f.Text, f.Client, f.Due), tone: "urgent", client: f.Client, followUp: &f})
		}
	}
	if len(t.Retainers) > 0 {
		section("Retainers to bill")
		for _, r := range t.Retainers {
			rows = append(rows, row{text: fmt.Sprintf("%s  %s, %s", r.Client, r.Title, monthName(r.Month)), tone: "warn", client: r.Client})
		}
	}
	if len(t.Quotes) > 0 {
		section("Quotes waiting")
		for _, q := range t.Quotes {
			tone := ""
			if q.Expired(t.Date) {
				tone = "warn"
			}
			rows = append(rows, row{text: fmt.Sprintf("%s  %s  %s, sent %s", q.Number, q.Client, q.Title, q.Sent), tone: tone, client: q.Client, quote: q.Number})
		}
	}
	if len(t.ThisWeek) > 0 {
		section("Due this week")
		for i := range t.ThisWeek {
			f := t.ThisWeek[i]
			rows = append(rows, row{text: fmt.Sprintf("%s  %s, due %s", f.Text, f.Client, f.Due), client: f.Client, followUp: &f})
		}
	}
	if len(t.Moves) > 0 {
		section("Worth a move?")
		for _, n := range t.Moves {
			rows = append(rows, row{text: fmt.Sprintf("%s  %s: %s?", n.Client, n.Reason, n.Suggest), tone: "dim", client: n.Client})
		}
	}
	if len(t.KeepInTouch) > 0 {
		section("Keep in touch")
		for _, n := range t.KeepInTouch {
			rows = append(rows, row{text: fmt.Sprintf("%s  %s", n.Client, n.Reason), tone: "dim", client: n.Client})
		}
	}
	if len(t.Engagements) > 0 {
		section("Active engagements")
		for _, e := range t.Engagements {
			rows = append(rows, row{text: fmt.Sprintf("%s  %s", e.Client, e.Title), tone: "dim", client: e.Client})
		}
	}
	return rows
}

func (m model) clientRows() []row {
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
		rows = append(rows, row{header: true, text: strings.ToUpper(status[:1]) + status[1:]})
		for _, c := range in {
			tone := ""
			if status == "cold" {
				tone = "dim"
			}
			rows = append(rows, row{text: fmt.Sprintf("%-12s %s", c.Slug, c.Name), tone: tone, client: c.Slug})
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
		tone := ""
		switch {
		case inv.Status == "draft":
			tone = "dim"
		case inv.Balance > 0 && inv.Due < m.today.Date:
			tone = "urgent"
		case inv.Status == "paid" || inv.FullyCredited():
			tone = "ok"
		}
		amount := gbp(inv.Total, inv.Currency)
		if inv.Kind == "credit" {
			amount = "-" + amount
		}
		rows = append(rows, row{text: fmt.Sprintf("%-22s %-9s %-10s %s", name, inv.Display(), inv.Client, amount), tone: tone, client: inv.Client, invoice: name})
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
		rows = append(rows, row{text: fmt.Sprintf("%-18s %-9s %-10s %s", name, q.Display(m.today.Date), q.Client, q.Title), tone: tone, client: q.Client, quote: name})
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

func monthName(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return t.Format("January 2006")
}
