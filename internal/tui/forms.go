package tui

import (
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Every input in the TUI is a huh form, shown in the detail pane so the
// list stays in view. Fields validate as you type, with the same parsers
// the CLI uses, so the two accept exactly the same input.
//
// A form's values live in a small struct allocated per form, never on the
// model: Bubble Tea copies the model on every update, and a form bound to
// the model's own fields would write into a copy that is thrown away.

// openForm shows a form. submit runs once it is completed; its status, or
// its error, goes to the status line.
func (m model) openForm(title string, f *huh.Form, submit func() (string, error)) (tea.Model, tea.Cmd) {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	f = f.WithTheme(huh.ThemeBase16()).WithKeyMap(keys).WithShowHelp(true).WithWidth(max(m.width-sideWidth-6, 30))
	m.form, m.formTitle, m.formSubmit = f, title, submit
	m.mode = modeForm
	return m, f.Init()
}

// updateForm hands every message to the open form, not only keys: huh moves
// between fields with messages of its own.
func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.form.Update(msg)
	if f, ok := next.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateAborted:
		m.mode, m.form = modeNormal, nil
		return m.say("Cancelled")
	case huh.StateCompleted:
		submit := m.formSubmit
		m.mode, m.form, m.formSubmit = modeNormal, nil, nil
		status, err := submit()
		if err != nil {
			return m.say(err.Error())
		}
		m.reload()
		return m.say(status)
	}
	return m, cmd
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is needed", what)
		}
		return nil
	}
}

// ---------------------------------------------------------------- time

func (m model) startTime() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("t logs time; select a client or an engagement first")
	}
	engagements := m.clientEngagements(r.client)
	if len(engagements) == 0 {
		return m.say(r.client + " has no engagement to log time to; add one with mavis engagement add")
	}
	v := &struct{ engagement, duration, what, date string }{engagement: engagements[0].Slug, date: "today"}
	var options []huh.Option[string]
	for _, e := range engagements {
		options = append(options, huh.NewOption(e.Title+"  ("+e.Slug+")", e.Slug))
		if e.Slug == r.engagement {
			v.engagement = e.Slug
		}
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Engagement").Options(options...).Value(&v.engagement),
		huh.NewInput().Title("Duration").Placeholder("1d, 0.5d, 3h, 45m or 1h30m").Value(&v.duration).
			Validate(func(s string) error {
				_, err := duration.Parse(s, m.s.DayMinutes)
				return err
			}),
		huh.NewInput().Title("What").Placeholder("optional").Value(&v.what),
		huh.NewInput().Title("Date").Placeholder("YYYY-MM-DD, today or yesterday").Value(&v.date).
			Validate(func(s string) error {
				_, err := m.s.ParseDay(s)
				return err
			}),
	))
	return m.openForm("Log time for "+r.client, f, func() (string, error) {
		e, err := m.s.AddTime(store.NewTime{Engagement: v.engagement, Duration: v.duration, What: v.what, Date: v.date})
		if err != nil {
			return "", err
		}
		status := fmt.Sprintf("Logged %s to %s on %s", e.Time, e.Engagement, e.Date)
		if by, err := m.s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
			status += "; that month is already on " + by + ", so this is not"
		}
		return status, nil
	})
}

// ---------------------------------------------------------------- a call

func (m model) startCall() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("l logs a call or meeting; select a client first")
	}
	client := r.client
	v := &struct{ kind, summary, followUp, due string }{kind: "call"}
	f := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Kind").Options(huh.NewOptions("call", "meeting", "email", "note")...).Value(&v.kind),
		huh.NewInput().Title("Summary").Placeholder("what happened").Value(&v.summary).Validate(required("A summary")),
		huh.NewInput().Title("Follow-up").Placeholder("optional: what happens next").Value(&v.followUp),
		huh.NewInput().Title("Due").Placeholder("optional: YYYY-MM-DD or +3d").Value(&v.due).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return nil
				}
				if strings.TrimSpace(v.followUp) == "" {
					return fmt.Errorf("a due date needs a follow-up")
				}
				_, err := m.s.ParseDue(s)
				return err
			}),
	))
	return m.openForm("Log for "+client, f, func() (string, error) {
		nl := store.NewLog{Kind: v.kind, Client: client, Summary: strings.TrimSpace(v.summary)}
		if fu := strings.TrimSpace(v.followUp); fu != "" {
			nl.FollowUps = []store.NewFollowUp{{Text: fu, Due: strings.TrimSpace(v.due)}}
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
	})
}

// ---------------------------------------------------------------- a note

func (m model) startNote() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("n adds a note to a client; select one first")
	}
	client := r.client
	v := &struct{ text string }{}
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Note for " + client).Value(&v.text).Validate(required("A note")),
	))
	return m.openForm("Note", f, func() (string, error) {
		if _, err := m.s.AddLog(store.NewLog{Kind: "note", Client: client, Summary: strings.TrimSpace(v.text)}); err != nil {
			return "", err
		}
		return "Noted against " + client, nil
	})
}

// ---------------------------------------------------------------- a move

func (m model) startMove() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("m moves a client; select one first")
	}
	client := r.client
	v := &struct{ status string }{status: "active"}
	for _, c := range m.clients {
		if c.Slug == client {
			v.status = c.Status
		}
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Move " + client + " to").
			Options(huh.NewOptions("active", "warm", "cold", "prospect")...).Value(&v.status),
	))
	return m.openForm("Move", f, func() (string, error) {
		c, changed, err := m.s.SetClientStatus(client, v.status)
		if err != nil {
			return "", err
		}
		if !changed {
			return c.Name + " is already " + v.status, nil
		}
		return c.Name + " is now " + v.status, nil
	})
}

// ---------------------------------------------------------------- paid

func (m model) startPaid() (tea.Model, tea.Cmd) {
	inv, ok := m.invoiceFor(m.selected())
	if !ok || inv.Kind != "invoice" || inv.Status != "issued" {
		return m.say("p marks an issued invoice paid; select one first")
	}
	v := &struct{ yes bool }{}
	f := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(fmt.Sprintf("Mark %s paid today, %s?", inv.Number, gbp(inv.Balance, inv.Currency))).
			Affirmative("Paid").Negative("Not yet").Value(&v.yes),
	))
	return m.openForm("Paid", f, func() (string, error) {
		if !v.yes {
			return "Left unpaid", nil
		}
		got, err := m.s.SetPaid(inv.Number, true, "")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s paid on %s", got.Number, got.Paid), nil
	})
}
