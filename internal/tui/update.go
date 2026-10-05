package tui

import (
	"github.com/JustSteveKing/mavis/internal/remind"
	"github.com/JustSteveKing/mavis/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == modeForm && m.form != nil {
		switch msg.(type) {
		case tickMsg:
			return m, tick() // no reload under someone filling in a form
		case clearStatusMsg:
			m.status = ""
			return m, nil
		}
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			// The form gets the pane's size, not the terminal's.
			m.width, m.height = ws.Width, ws.Height
			w, h := m.formSize()
			msg = tea.WindowSizeMsg{Width: w, Height: h}
		}
		return m.updateForm(msg)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// Never reload under someone who is typing or confirming.
		if m.mode == modeNormal {
			m.reload()
		}
		return m, tick()

	case clearStatusMsg:
		m.status = ""
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeHelp:
			m.mode = modeNormal
			return m, nil
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "tab":
		m.focus = 1 - m.focus
		return m, nil
	case "1":
		m.focus = focusViews
		return m, nil
	case "2":
		m.focus = focusList
		return m, nil
	case "R":
		m.reload()
		return m.say("Reloaded")
	}

	if msg.String() == "c" {
		return m.startNewClient()
	}
	if m.focus == focusViews {
		switch msg.String() {
		case "j", "down":
			m.switchView((m.view + 1) % viewCount)
		case "k", "up":
			m.switchView((m.view + viewCount - 1) % viewCount)
		case "enter", "l", "right":
			m.focus = focusList
		}
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "h", "left":
		m.focus = focusViews
	case "enter":
		m.open()
	case "x":
		return m.tickFollowUp()
	case "n":
		return m.startNote()
	case "p":
		return m.startPaid()
	case "r":
		return m.previewReminder()
	case "t":
		return m.startTime()
	case "d":
		return m.startDelivery()
	case "l":
		return m.startCall()
	case "m":
		return m.startMove()
	case "c":
		return m.startNewClient()
	case "e":
		return m.startNewEngagement()
	case "i":
		return m.startDraftInvoice()
	}
	return m, nil
}

func (m *model) switchView(v viewKind) {
	m.view = v
	m.detailNote = ""
	m.buildRows()
}

// open jumps from a Today row to the record behind it: an invoice to the
// invoice, a quote to the quote, anything else to its client.
func (m *model) open() {
	r := m.selected()
	if r == nil || m.view != viewToday {
		return
	}
	target, key := viewClients, r.client
	switch {
	case r.invoice != "":
		target, key = viewInvoices, r.invoice
	case r.quote != "":
		target, key = viewQuotes, r.quote
	}
	m.switchView(target)
	for i, row := range m.rows {
		if row.selectable() && (row.client == key && target == viewClients || row.invoice == key || row.quote == key) {
			m.cursor[m.view] = i
			break
		}
	}
}

func (m model) say(s string) (tea.Model, tea.Cmd) {
	m.status = s
	return m, clearStatus()
}

func (m model) tickFollowUp() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.followUp == nil {
		return m.say("x ticks a follow-up off; select one in Today")
	}
	f, err := m.s.CompleteFollowUpExact(r.followUp.Log, r.followUp.Text)
	if err != nil {
		return m.say(err.Error())
	}
	m.reload()
	return m.say("Done: " + f.Text)
}

// invoiceFor is the invoice a row is about, if it is one that is owed.
func (m model) invoiceFor(r *row) (store.Invoice, bool) {
	if r == nil || r.invoice == "" {
		return store.Invoice{}, false
	}
	for _, inv := range m.invoices {
		if inv.Number == r.invoice || inv.Slug == r.invoice {
			return inv, true
		}
	}
	return store.Invoice{}, false
}

// previewReminder shows the next reminder's text in the detail pane. It is a
// preview: recording one as sent stays a deliberate act at the CLI.
func (m model) previewReminder() (tea.Model, tea.Cmd) {
	inv, ok := m.invoiceFor(m.selected())
	if !ok {
		return m.say("r previews a reminder; select an overdue invoice")
	}
	sent, err := m.s.Reminders(inv.Number)
	if err != nil {
		return m.say(err.Error())
	}
	var dates []string
	for _, r := range sent {
		dates = append(dates, r.Date[:10])
	}
	contact := ""
	for _, c := range m.clients {
		if c.Slug == inv.Client {
			contact = c.Contact
		}
	}
	signoff := m.o.Signoff
	if signoff == "" {
		signoff = inv.From.Name
	}
	r, err := remind.Write(remind.Input{Invoice: inv, Contact: contact, Signoff: signoff, Earlier: dates, Today: m.today.Date, Statutory: m.o.Statutory})
	if err != nil {
		return m.say(err.Error())
	}
	m.detailNote = "Subject: " + r.Subject + "\n\n" + r.Body + "\n\n" +
		styleHint.Render("Once sent: mavis invoice remind "+inv.Number+" --sent")
	return m, nil
}

// clientEngagements lists a client's engagements that time can go on,
// active first.
func (m model) clientEngagements(client string) []store.Engagement {
	var active, rest []store.Engagement
	for _, e := range m.engagements {
		if e.Client != client || e.Status == "done" {
			continue
		}
		if e.Status == "active" {
			active = append(active, e)
		} else {
			rest = append(rest, e)
		}
	}
	return append(active, rest...)
}
