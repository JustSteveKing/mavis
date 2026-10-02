package tui

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/remind"
	"github.com/JustSteveKing/mavis/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case modeNote:
			return m.updateNote(msg)
		case modeConfirmPaid:
			return m.updateConfirmPaid(msg)
		case modeForm:
			return m.updateForm(msg)
		case modeMove:
			return m.updateMove(msg)
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
	case "l":
		return m.startCall()
	case "m":
		return m.startMove()
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

func (m model) startNote() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("n adds a note to a client; select one first")
	}
	m.mode = modeNote
	m.input.Prompt = "note for " + r.client + "> "
	m.input.SetValue("")
	m.input.Focus()
	return m, nil
}

func (m model) updateNote(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m.say("Note dropped")
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		m.input.Blur()
		r := m.selected()
		if text == "" || r == nil {
			return m.say("Nothing noted")
		}
		if _, err := m.s.AddLog(store.NewLog{Kind: "note", Client: r.client, Summary: text}); err != nil {
			return m.say(err.Error())
		}
		m.reload()
		return m.say("Noted against " + r.client)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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

func (m model) startPaid() (tea.Model, tea.Cmd) {
	inv, ok := m.invoiceFor(m.selected())
	if !ok || inv.Kind != "invoice" || inv.Status != "issued" {
		return m.say("p marks an issued invoice paid; select one first")
	}
	m.mode = modeConfirmPaid
	return m, nil
}

func (m model) updateConfirmPaid(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	if msg.String() != "y" {
		return m.say("Left unpaid")
	}
	inv, ok := m.invoiceFor(m.selected())
	if !ok {
		return m.say("That invoice has gone")
	}
	got, err := m.s.SetPaid(inv.Number, true, "")
	if err != nil {
		return m.say(err.Error())
	}
	m.reload()
	return m.say(fmt.Sprintf("%s paid on %s", got.Number, got.Paid))
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

// ---------------------------------------------------------------- forms

func (m model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, status, cmd := m.form.update(msg)
	if !done {
		return m, cmd
	}
	m.mode, m.form = modeNormal, nil
	m.reload()
	next, clear := m.say(status)
	return next, tea.Batch(cmd, clear)
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

func (m model) startTime() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("t logs time; select a client or an engagement first")
	}
	engagements := m.clientEngagements(r.client)
	if len(engagements) == 0 {
		return m.say(r.client + " has no engagement to log time to; add one with mavis engagement add")
	}
	var slugs, labels []string
	selected := 0
	for i, e := range engagements {
		slugs = append(slugs, e.Slug)
		labels = append(labels, e.Title+"  ("+e.Slug+")")
		if e.Slug == r.engagement {
			selected = i
		}
	}
	m.form = newForm("Log time for "+r.client, func(v map[string]string) (string, error) {
		e, err := m.s.AddTime(store.NewTime{Engagement: v["engagement"], Duration: v["duration"], What: v["what"], Date: v["date"]})
		if err != nil {
			return "", err
		}
		status := fmt.Sprintf("Logged %s to %s on %s", e.Time, e.Engagement, e.Date)
		if by, err := m.s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
			status += "; that month is already on " + by + ", so this is not"
		}
		return status, nil
	},
		choiceField("engagement", "Engagement", slugs, labels, selected),
		textField("duration", "Duration", "1d, 0.5d, 3h, 45m or 1h30m", ""),
		textField("what", "What", "optional", ""),
		textField("date", "Date", "YYYY-MM-DD, today or yesterday", "today"),
	)
	m.form.fields[2].optional = true
	m.form.focusField(1) // the engagement is usually right already
	m.mode = modeForm
	return m, nil
}

func (m model) startCall() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("l logs a call or meeting; select a client first")
	}
	client := r.client
	m.form = newForm("Log for "+client, func(v map[string]string) (string, error) {
		nl := store.NewLog{Kind: v["kind"], Client: client, Summary: v["summary"]}
		if v["follow_up"] != "" {
			nl.FollowUps = []store.NewFollowUp{{Text: v["follow_up"], Due: v["due"]}}
		} else if v["due"] != "" {
			return "", fmt.Errorf("a due date needs a follow-up")
		}
		l, err := m.s.AddLog(nl)
		if err != nil {
			return "", err
		}
		status := "Logged " + l.Kind + " with " + client
		if len(l.FollowUps) > 0 {
			status += ", follow-up: " + l.FollowUps[0].Text
		}
		return status, nil
	},
		choiceField("kind", "Kind", []string{"call", "meeting", "email", "note"}, nil, 0),
		textField("summary", "Summary", "what happened", ""),
		textField("follow_up", "Follow-up", "optional: what happens next", ""),
		textField("due", "Due", "optional: YYYY-MM-DD or +3d", ""),
	)
	m.form.fields[2].optional = true
	m.form.fields[3].optional = true
	m.form.focusField(1)
	m.mode = modeForm
	return m, nil
}

// ---------------------------------------------------------------- moves

func (m model) startMove() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("m moves a client; select one first")
	}
	m.mode = modeMove
	return m, nil
}

var moveKeys = map[string]string{"a": "active", "w": "warm", "c": "cold", "p": "prospect"}

func (m model) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	status, ok := moveKeys[msg.String()]
	r := m.selected()
	if !ok || r == nil {
		return m.say("Not moved")
	}
	c, changed, err := m.s.SetClientStatus(r.client, status)
	if err != nil {
		return m.say(err.Error())
	}
	m.reload()
	if !changed {
		return m.say(c.Name + " is already " + status)
	}
	return m.say(c.Name + " is now " + status)
}
