// Package mcpserver exposes mavis to AI agents over MCP.
//
// Every tool is a thin wrapper over internal/store, and the store it is given
// stamps `by: agent` on what it creates. The line this package holds is in
// instructions.go: agents read, keep records and draft; irreversible and
// outward-facing actions (issuing, sending, marking paid, answering quotes)
// are not here at all, so no agent can take them by mistake.
//
// Tools take exact slugs and numbers, never fuzzy names. A person who gets
// the wrong match from `mavis client show acm` sees it and retypes; an agent
// that gets the wrong one acts on it. The list tools are how an agent finds
// the identifier it needs.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/JustSteveKing/mavis/internal/remind"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options are the parts of the config tools need.
type Options struct {
	Quiet     store.Quiet
	Signoff   string // your name, to sign reminders
	Statutory bool   // cite the Late Payment Act in final reminders
}

// New builds a server over a store. The store should have Actor set, so
// what agents create is marked as theirs.
func New(s *store.Store, o Options, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "mavis", Title: "mavis", Version: version}, &mcp.ServerOptions{
		Instructions: instructions,
	})
	t := &tools{s: s, o: o}
	t.registerReads(srv)
	t.registerRecords(srv)
	t.registerDrafts(srv)
	return srv
}

type tools struct {
	s *store.Store
	o Options
}

var (
	readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}
	no       = false
	// writes change files, but nothing they do cannot be edited back.
	writes = &mcp.ToolAnnotations{DestructiveHint: &no}
)

// ---------------------------------------------------------------- exactness

func (t *tools) client(slug string) (store.Client, error) {
	c, err := t.s.ResolveClient(slug)
	if err != nil || c.Slug != slug {
		return store.Client{}, fmt.Errorf("no client with slug %q; list_clients gives the slugs", slug)
	}
	return c, nil
}

func (t *tools) engagement(slug string) (store.Engagement, error) {
	e, err := t.s.ResolveEngagement(slug)
	if err != nil || e.Slug != slug {
		return store.Engagement{}, fmt.Errorf("no engagement with slug %q; list_engagements gives the slugs, which are <client>-<name>", slug)
	}
	return e, nil
}

func (t *tools) invoice(ref string) (store.Invoice, error) {
	inv, err := t.s.ResolveInvoice(ref)
	if err != nil || (inv.Number != ref && inv.Slug != ref) {
		return store.Invoice{}, fmt.Errorf("no invoice %q; list_invoices gives numbers, and slugs for drafts", ref)
	}
	return inv, nil
}

func (t *tools) quote(ref string) (store.Quote, error) {
	q, err := t.s.ResolveQuote(ref)
	if err != nil || (q.Number != ref && q.Slug != ref) {
		return store.Quote{}, fmt.Errorf("no quote %q; list_quotes gives numbers, and slugs for drafts", ref)
	}
	return q, nil
}

// ---------------------------------------------------------------- reads

type empty struct{}

type chase struct {
	Invoice       string `json:"invoice"`
	RemindersSent int    `json:"reminders_sent"`
	LastReminder  string `json:"last_reminder,omitempty"`
	NextReminder  int    `json:"next_reminder"`
	NextDue       bool   `json:"next_due"`
	NextFrom      string `json:"next_from,omitempty"`
}

type todayOut struct {
	store.Today
	Chase []chase `json:"chase"`
}

type clientsIn struct {
	Status string `json:"status,omitempty" jsonschema:"only clients with this status: prospect, active, warm or cold"`
}
type clientsOut struct {
	Clients []store.Client `json:"clients"`
	Count   int            `json:"count"`
}

type slugIn struct {
	Slug string `json:"slug" jsonschema:"exact slug; required"`
}
type clientOut struct {
	Client      store.Client       `json:"client"`
	Engagements []store.Engagement `json:"engagements"`
	FollowUps   []store.FollowUp   `json:"open_follow_ups"`
	RecentLog   []store.LogEntry   `json:"recent_log" jsonschema:"newest first, at most ten"`
}

type engagementsIn struct {
	Client string `json:"client,omitempty" jsonschema:"exact client slug"`
	Status string `json:"status,omitempty" jsonschema:"proposed, active, paused or done"`
}
type engagementsOut struct {
	Engagements []store.Engagement `json:"engagements"`
	Count       int                `json:"count"`
}

type followUpsIn struct {
	Client  string `json:"client,omitempty" jsonschema:"exact client slug"`
	Overdue bool   `json:"overdue,omitempty" jsonschema:"only those past their due date"`
}
type followUpsOut struct {
	FollowUps []store.FollowUp `json:"follow_ups" jsonschema:"open ones only, soonest due first; log is the entry each is in"`
	Count     int              `json:"count"`
}

type timeIn struct {
	Month      string `json:"month,omitempty" jsonschema:"YYYY-MM; defaults to this month; 'all' for every month"`
	Client     string `json:"client,omitempty" jsonschema:"exact client slug"`
	Engagement string `json:"engagement,omitempty" jsonschema:"exact engagement slug"`
}
type timeOut struct {
	Entries      []store.TimeEntry `json:"entries"`
	TotalMinutes int               `json:"total_minutes"`
	DayMinutes   int               `json:"day_minutes" jsonschema:"minutes in a working day, for converting to days"`
}

type statsIn struct {
	Month string `json:"month,omitempty" jsonschema:"YYYY-MM; defaults to this month"`
	Year  string `json:"year,omitempty" jsonschema:"YYYY, instead of a month"`
	From  string `json:"from,omitempty" jsonschema:"YYYY-MM-DD, with to, instead of a month"`
	To    string `json:"to,omitempty" jsonschema:"YYYY-MM-DD"`
}

type invoicesIn struct {
	Client string `json:"client,omitempty" jsonschema:"exact client slug"`
	Status string `json:"status,omitempty" jsonschema:"draft, issued or paid"`
}
type invoicesOut struct {
	Invoices []store.Invoice `json:"invoices"`
	Count    int             `json:"count"`
}
type refIn struct {
	Ref string `json:"ref" jsonschema:"an exact number such as INV-2026-001, or a draft's slug; required"`
}

type quotesIn struct {
	Client string `json:"client,omitempty" jsonschema:"exact client slug"`
	Status string `json:"status,omitempty" jsonschema:"draft, sent, accepted or declined"`
}
type quotesOut struct {
	Quotes []store.Quote `json:"quotes"`
	Count  int           `json:"count"`
}

type retainersOut struct {
	Due   []store.RetainerDue `json:"due" jsonschema:"finished retainer months no invoice covers yet"`
	Count int                 `json:"count"`
}

func (t *tools) registerReads(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_today",
		Description: "What needs attention now: overdue invoices and how far each has been chased, follow-ups overdue and due this week, retainer months to bill, quotes waiting on an answer, active engagements, and clients worth a move or a call. Start here.",
		Annotations: readOnly,
	}, func(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, todayOut, error) {
		td, _, err := t.s.Today(t.o.Quiet)
		if err != nil {
			return nil, todayOut{}, err
		}
		out := todayOut{Today: td, Chase: []chase{}}
		for _, inv := range td.Unpaid {
			sent, err := t.s.Reminders(inv.Number)
			if err != nil {
				return nil, todayOut{}, err
			}
			var dates []string
			for _, r := range sent {
				dates = append(dates, r.Date[:10])
			}
			next, due, from := remind.Next(inv, dates, td.Date)
			c := chase{Invoice: inv.Number, RemindersSent: len(dates), NextReminder: next, NextDue: due, NextFrom: from}
			if len(dates) > 0 {
				c.LastReminder = dates[len(dates)-1]
			}
			out.Chase = append(out.Chase, c)
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_clients", Description: "List clients with their slugs, status and contact.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in clientsIn) (*mcp.CallToolResult, clientsOut, error) {
			all, _, err := t.s.Clients()
			if err != nil {
				return nil, clientsOut{}, err
			}
			out := clientsOut{Clients: []store.Client{}}
			for _, c := range all {
				if in.Status == "" || c.Status == in.Status {
					out.Clients = append(out.Clients, c)
				}
			}
			out.Count = len(out.Clients)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_client", Description: "One client in full: details, engagements, open follow-ups and recent log entries.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in slugIn) (*mcp.CallToolResult, clientOut, error) {
			c, err := t.client(in.Slug)
			if err != nil {
				return nil, clientOut{}, err
			}
			out := clientOut{Client: c, Engagements: []store.Engagement{}, FollowUps: []store.FollowUp{}, RecentLog: []store.LogEntry{}}
			engagements, _, err := t.s.Engagements()
			if err != nil {
				return nil, clientOut{}, err
			}
			for _, e := range engagements {
				if e.Client == c.Slug {
					out.Engagements = append(out.Engagements, e)
				}
			}
			logs, _, err := t.s.Logs()
			if err != nil {
				return nil, clientOut{}, err
			}
			for i := len(logs) - 1; i >= 0; i-- {
				l := logs[i]
				if l.Client != c.Slug {
					continue
				}
				if len(out.RecentLog) < 10 {
					out.RecentLog = append(out.RecentLog, l)
				}
				for _, f := range l.FollowUps {
					if !f.Done {
						out.FollowUps = append(out.FollowUps, f)
					}
				}
			}
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_engagements", Description: "List engagements, pieces of work for a client, with their slugs, basis and rate.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in engagementsIn) (*mcp.CallToolResult, engagementsOut, error) {
			all, _, err := t.s.Engagements()
			if err != nil {
				return nil, engagementsOut{}, err
			}
			out := engagementsOut{Engagements: []store.Engagement{}}
			for _, e := range all {
				if (in.Client == "" || e.Client == in.Client) && (in.Status == "" || e.Status == in.Status) {
					out.Engagements = append(out.Engagements, e)
				}
			}
			out.Count = len(out.Engagements)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_follow_ups", Description: "List open follow-ups, soonest due first. Each names the log entry it is in, which complete_follow_up needs.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in followUpsIn) (*mcp.CallToolResult, followUpsOut, error) {
			open, _, err := t.s.FollowUps()
			if err != nil {
				return nil, followUpsOut{}, err
			}
			today := t.s.Now().Format("2006-01-02")
			out := followUpsOut{FollowUps: []store.FollowUp{}}
			for _, f := range open {
				if in.Client != "" && f.Client != in.Client {
					continue
				}
				if in.Overdue && (f.Due == "" || f.Due >= today) {
					continue
				}
				out.FollowUps = append(out.FollowUps, f)
			}
			out.Count = len(out.FollowUps)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_time", Description: "List time entries for a month, with the total in minutes.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in timeIn) (*mcp.CallToolResult, timeOut, error) {
			all, _, err := t.s.TimeEntries()
			if err != nil {
				return nil, timeOut{}, err
			}
			month := in.Month
			if month == "" {
				month = t.s.Now().Format("2006-01")
			}
			out := timeOut{Entries: []store.TimeEntry{}, DayMinutes: t.s.DayMinutes}
			for _, e := range all {
				if month != "all" && !strings.HasPrefix(e.Date, month) {
					continue
				}
				if (in.Client != "" && e.Client != in.Client) || (in.Engagement != "" && e.Engagement != in.Engagement) {
					continue
				}
				out.Entries = append(out.Entries, e)
				out.TotalMinutes += e.Minutes
			}
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_stats", Description: "Time and its value at the engagement rates for a period, plus what invoices and quotes say once any are issued or sent. Value is not revenue.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in statsIn) (*mcp.CallToolResult, store.Stats, error) {
			var p store.Period
			var err error
			switch {
			case in.Year != "":
				p, err = store.YearPeriod(in.Year)
			case in.From != "":
				to := in.To
				if to == "" {
					to = t.s.Now().Format("2006-01-02")
				}
				p = store.Period{From: in.From, To: to}
			default:
				month := in.Month
				if month == "" {
					month = t.s.Now().Format("2006-01")
				}
				p, err = store.MonthPeriod(month)
			}
			if err != nil {
				return nil, store.Stats{}, err
			}
			st, _, err := t.s.Stats(p)
			return nil, st, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_invoices", Description: "List invoices and credit notes. balance_pence is what is still owed.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in invoicesIn) (*mcp.CallToolResult, invoicesOut, error) {
			all, _, err := t.s.Invoices()
			if err != nil {
				return nil, invoicesOut{}, err
			}
			out := invoicesOut{Invoices: []store.Invoice{}}
			for _, inv := range all {
				if (in.Client == "" || inv.Client == in.Client) && (in.Status == "" || inv.Status == in.Status) {
					out.Invoices = append(out.Invoices, inv)
				}
			}
			out.Count = len(out.Invoices)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_invoice", Description: "One invoice or credit note, with its lines and totals.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, store.Invoice, error) {
			inv, err := t.invoice(in.Ref)
			return nil, inv, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_quotes", Description: "List quotes. A sent quote past valid_until is expired.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in quotesIn) (*mcp.CallToolResult, quotesOut, error) {
			all, _, err := t.s.Quotes()
			if err != nil {
				return nil, quotesOut{}, err
			}
			out := quotesOut{Quotes: []store.Quote{}}
			for _, q := range all {
				if (in.Client == "" || q.Client == in.Client) && (in.Status == "" || q.Status == in.Status) {
					out.Quotes = append(out.Quotes, q)
				}
			}
			out.Count = len(out.Quotes)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "get_quote", Description: "One quote, with its scope, lines and totals.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, store.Quote, error) {
			q, err := t.quote(in.Ref)
			return nil, q, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "list_retainers_due", Description: "Retainer months that have finished and are on no invoice. Retainers bill in arrears.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, retainersOut, error) {
			due, err := t.s.RetainersDue()
			if err != nil {
				return nil, retainersOut{}, err
			}
			if due == nil {
				due = []store.RetainerDue{}
			}
			return nil, retainersOut{Due: due, Count: len(due)}, nil
		})
}
