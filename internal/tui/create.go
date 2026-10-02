package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Creating records from the TUI goes through the same store calls as the
// CLI, so a client made here is the same file `mavis client add` makes,
// guards included.

func optionalAmount(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	_, err := money.Parse(s)
	return err
}

// ---------------------------------------------------------------- client

func (m model) startNewClient() (tea.Model, tea.Cmd) {
	v := &struct{ name, slug, status, contact, email, phone string }{status: "active"}
	taken := map[string]bool{}
	for _, c := range m.clients {
		taken[c.Slug] = true
	}
	slug := func() string {
		if s := strings.TrimSpace(v.slug); s != "" {
			return s
		}
		return store.Slugify(v.name)
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Placeholder("company or person, e.g. Acme Ltd").Value(&v.name).Validate(required("A name")),
		huh.NewInput().Title("Slug").Description("the file name, and how [[links]] reach them; leave blank for the suggestion").
			PlaceholderFunc(func() string { return store.Slugify(v.name) }, &v.name).
			Value(&v.slug).
			Validate(func(string) error {
				s := slug()
				switch {
				case s == "":
					return fmt.Errorf("a slug is needed")
				case !store.ValidSlug(s):
					return fmt.Errorf("%q: lowercase letters, numbers and single hyphens", s)
				case taken[s]:
					return fmt.Errorf("there is already a client called %s", s)
				}
				return nil
			}),
		huh.NewSelect[string]().Title("Status").Options(huh.NewOptions("active", "prospect", "warm", "cold")...).Value(&v.status),
		huh.NewInput().Title("Contact").Placeholder("optional").Value(&v.contact),
		huh.NewInput().Title("Email").Placeholder("optional").Value(&v.email),
		huh.NewInput().Title("Phone").Placeholder("optional").Value(&v.phone),
	))
	return m.openForm("New client", f, func() (outcome, error) {
		c, err := m.s.AddClient(store.NewClient{
			Slug: slug(), Name: strings.TrimSpace(v.name), Status: v.status,
			Contact: strings.TrimSpace(v.contact), Email: strings.TrimSpace(v.email), Phone: strings.TrimSpace(v.phone),
		})
		if err != nil {
			return outcome{}, err
		}
		return outcome{
			status: "Added " + c.Name + " (" + c.Slug + ")",
			then: func(m *model) {
				m.switchView(viewClients)
				m.selectWhere(func(r row) bool { return r.client == c.Slug })
			},
		}, nil
	})
}

// ---------------------------------------------------------------- engagement

func (m model) startNewEngagement() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("e adds an engagement to a client; select one first")
	}
	client := r.client
	v := &struct{ title, name, status, basis, rate, budget, start string }{status: "active", basis: "day", start: "today"}
	name := func() string {
		if s := strings.TrimSpace(v.name); s != "" {
			return s
		}
		return store.Slugify(v.title)
	}
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Title").Placeholder("what the work is, e.g. Reporting module").Value(&v.title).Validate(required("A title")),
		huh.NewInput().Title("Name").Description("short; the file becomes "+client+"-<name>; leave blank for the suggestion").
			PlaceholderFunc(func() string { return store.Slugify(v.title) }, &v.title).
			Value(&v.name).
			Validate(func(string) error {
				if s := name(); !store.ValidSlug(s) {
					return fmt.Errorf("%q: lowercase letters, numbers and single hyphens", s)
				}
				return nil
			}),
		huh.NewSelect[string]().Title("Status").Options(huh.NewOptions("active", "proposed")...).Value(&v.status),
		huh.NewSelect[string]().Title("Basis").Options(
			huh.NewOption("day rate", "day"), huh.NewOption("hourly", "hourly"),
			huh.NewOption("fixed price", "fixed"), huh.NewOption("monthly retainer", "retainer"),
			huh.NewOption("not yet decided", ""),
		).Value(&v.basis),
		huh.NewInput().Title("Rate").Description("per day or hour, or per month for a retainer; optional").Placeholder("e.g. 650").Value(&v.rate).Validate(optionalAmount),
		huh.NewInput().Title("Budget").Description("the agreed price, for fixed-price work; optional").Value(&v.budget).Validate(optionalAmount),
		huh.NewInput().Title("Start").Placeholder("YYYY-MM-DD, today or yesterday").Value(&v.start).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return nil
				}
				_, err := m.s.ParseDay(s)
				return err
			}),
	))
	return m.openForm("New engagement for "+client, f, func() (outcome, error) {
		start := ""
		if strings.TrimSpace(v.start) != "" {
			var err error
			if start, err = m.s.ParseDay(v.start); err != nil {
				return outcome{}, err
			}
		}
		e, err := m.s.AddEngagement(store.NewEngagement{
			Client: client, Name: name(), Title: strings.TrimSpace(v.title), Status: v.status,
			Basis: v.basis, Rate: strings.TrimSpace(v.rate), Budget: strings.TrimSpace(v.budget), Start: start,
		})
		if err != nil {
			return outcome{}, err
		}
		return done("Added " + e.Title + " (" + e.Slug + ")"), nil
	})
}

// ---------------------------------------------------------------- invoice

func (m model) startDraftInvoice() (tea.Model, tea.Cmd) {
	r := m.selected()
	if r == nil || r.client == "" {
		return m.say("i drafts an invoice for a client; select one first")
	}
	client := r.client
	today, _ := time.Parse("2006-01-02", m.today.Date)
	// Last month by default: billing usually follows the month.
	v := &struct{ month string }{month: today.AddDate(0, -1, 0).Format("2006-01")}
	f := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Bill " + client + " for which month?").
			Description("one line per engagement: day and hourly time at its rate, and retainers that ran").
			Placeholder("YYYY-MM").Value(&v.month).
			Validate(func(s string) error {
				if _, err := time.Parse("2006-01", strings.TrimSpace(s)); err != nil {
					return fmt.Errorf("use YYYY-MM, e.g. %s", today.Format("2006-01"))
				}
				return nil
			}),
	))
	return m.openForm("Draft an invoice", f, func() (outcome, error) {
		inv, skipped, err := m.s.AddInvoice(store.NewInvoice{Client: client, Month: strings.TrimSpace(v.month)})
		if err != nil {
			return outcome{}, err
		}
		status := fmt.Sprintf("Drafted %s: %s; check it, then mavis invoice issue %s", inv.Slug, gbp(inv.Total, inv.Currency), inv.Slug)
		if len(skipped) > 0 {
			var left []string
			for _, sk := range skipped {
				left = append(left, sk.Engagement+" ("+sk.Reason+")")
			}
			status += ". Left off: " + strings.Join(left, ", ")
		}
		return outcome{
			status: status,
			then: func(m *model) {
				m.switchView(viewInvoices)
				m.selectWhere(func(r row) bool { return r.invoice == inv.Slug })
			},
		}, nil
	})
}
