package mcpserver

import (
	"context"
	"errors"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type addClientIn struct {
	Slug    string `json:"slug" jsonschema:"short file name and link, lowercase with hyphens, e.g. acme; required"`
	Name    string `json:"name,omitempty" jsonschema:"company or person name"`
	Status  string `json:"status,omitempty" jsonschema:"prospect, active, warm or cold; defaults to active"`
	Contact string `json:"contact,omitempty" jsonschema:"primary contact's name"`
	Email   string `json:"email,omitempty"`
	Phone   string `json:"phone,omitempty"`
}

type updateClientIn struct {
	Slug           string    `json:"slug" jsonschema:"exact client slug; required"`
	Name           *string   `json:"name,omitempty"`
	Contact        *string   `json:"contact,omitempty"`
	Email          *string   `json:"email,omitempty"`
	Phone          *string   `json:"phone,omitempty"`
	Currency       *string   `json:"currency,omitempty" jsonschema:"three letters, e.g. GBP"`
	TermsDays      *int      `json:"terms_days,omitempty" jsonschema:"payment terms in days"`
	Address        *[]string `json:"address,omitempty" jsonschema:"REPLACES the address; one line per item, in order"`
	Country        *string   `json:"country,omitempty" jsonschema:"two letters, e.g. GB"`
	VATNumber      *string   `json:"vat_number,omitempty"`
	PeppolID       *string   `json:"peppol_id,omitempty" jsonschema:"scheme:value, e.g. 9932:GB123456789; never guess it from a VAT number"`
	BuyerReference *string   `json:"buyer_reference,omitempty" jsonschema:"the reference they want on invoices, e.g. a PO number"`
	Invoicing      *string   `json:"invoicing,omitempty" jsonschema:"where the client is invoiced when not by mavis, e.g. FreeAgent or Upwork; mavis then drafts no invoices for them. Set only when the human says so; mavis clears it"`
}

type moveIn struct {
	Slug   string `json:"slug" jsonschema:"exact slug; required"`
	Status string `json:"status" jsonschema:"the status to move to; required"`
}

type addEngagementIn struct {
	Client string `json:"client" jsonschema:"exact client slug; required"`
	Name   string `json:"name" jsonschema:"short name; the slug becomes <client>-<name>; required"`
	Title  string `json:"title,omitempty" jsonschema:"what the work is"`
	Status string `json:"status,omitempty" jsonschema:"proposed, active, paused or done; defaults to active"`
	Basis  string `json:"basis,omitempty" jsonschema:"day, hourly, fixed, retainer, or item for work paid per thing delivered"`
	Rate   string `json:"rate,omitempty" jsonschema:"decimal string; per day or hour, per item for item work, or per month for a retainer"`
	Budget string `json:"budget,omitempty" jsonschema:"decimal string; the agreed price of fixed-price work"`
	Unit   string `json:"unit,omitempty" jsonschema:"item work only: what one item is, singular, e.g. article; defaults to item"`
	Start  string `json:"start,omitempty" jsonschema:"YYYY-MM-DD"`
}

type followUpIn struct {
	Text string `json:"text" jsonschema:"what has to happen; required"`
	Due  string `json:"due,omitempty" jsonschema:"YYYY-MM-DD, or +3d / +2w from today"`
}

type logIn struct {
	Kind       string       `json:"kind" jsonschema:"call, meeting, email or note; required"`
	Client     string       `json:"client" jsonschema:"exact client slug; required"`
	Summary    string       `json:"summary" jsonschema:"what happened, briefly and factually; required"`
	Engagement string       `json:"engagement,omitempty" jsonschema:"exact engagement slug it was about"`
	With       []string     `json:"with,omitempty" jsonschema:"who was there"`
	Date       string       `json:"date,omitempty" jsonschema:"YYYY-MM-DD or YYYY-MM-DDTHH:MM; defaults to now"`
	FollowUps  []followUpIn `json:"follow_ups,omitempty" jsonschema:"anything that has to happen next"`
}

type completeIn struct {
	Log  string `json:"log" jsonschema:"the log entry the follow-up is in, as list_follow_ups gives it; required"`
	Text string `json:"text" jsonschema:"the follow-up's exact text; required"`
}

type timeEntryIn struct {
	Engagement string `json:"engagement" jsonschema:"exact engagement slug; required"`
	Duration   string `json:"duration" jsonschema:"1d, 0.5d, 3h, 45m or 1h30m; required"`
	What       string `json:"what,omitempty" jsonschema:"what the time was spent on"`
	Date       string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, today or yesterday; defaults to today"`
}

type movedOut struct {
	Client     *store.Client     `json:"client,omitempty"`
	Engagement *store.Engagement `json:"engagement,omitempty"`
	Changed    bool              `json:"changed"`
}

func (t *tools) registerRecords(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "add_client", Description: "Add a client. Refuses a slug that another note in the folder already uses.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in addClientIn) (*mcp.CallToolResult, store.Client, error) {
			c, err := t.s.AddClient(store.NewClient{Slug: in.Slug, Name: in.Name, Status: in.Status, Contact: in.Contact, Email: in.Email, Phone: in.Phone})
			return nil, c, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "update_client", Description: "Change some of a client's details. Fields left out are untouched; an empty string removes one. Anything the human added to the note is kept.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in updateClientIn) (*mcp.CallToolResult, store.Client, error) {
			if _, err := t.client(in.Slug); err != nil {
				return nil, store.Client{}, err
			}
			u := store.ClientUpdate{
				Name: in.Name, Contact: in.Contact, Email: in.Email, Phone: in.Phone, Currency: in.Currency,
				Country: in.Country, VATNumber: in.VATNumber, PeppolID: in.PeppolID, BuyerReference: in.BuyerReference,
				Invoicing: in.Invoicing, TermsDays: in.TermsDays,
			}
			if in.Address != nil {
				u.Address = *in.Address
				if u.Address == nil {
					u.Address = []string{}
				}
			}
			c, err := t.s.SetClient(in.Slug, u)
			return nil, c, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "move_client", Description: "Move a client to prospect, active, warm or cold. Only when the human asks: get_today's suggestions are for them to decide, not you.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, movedOut, error) {
			if _, err := t.client(in.Slug); err != nil {
				return nil, movedOut{}, err
			}
			c, changed, err := t.s.SetClientStatus(in.Slug, in.Status)
			return nil, movedOut{Client: &c, Changed: changed}, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "add_engagement", Description: "Add an engagement, a piece of work for a client. Basis and rate are optional until time or invoicing needs them.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in addEngagementIn) (*mcp.CallToolResult, store.Engagement, error) {
			if _, err := t.client(in.Client); err != nil {
				return nil, store.Engagement{}, err
			}
			e, err := t.s.AddEngagement(store.NewEngagement{Client: in.Client, Name: in.Name, Title: in.Title, Status: in.Status, Basis: in.Basis, Rate: in.Rate, Budget: in.Budget, Unit: in.Unit, Start: in.Start})
			return nil, e, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "move_engagement", Description: "Move an engagement to proposed, active, paused or done. Done stamps an end date.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, movedOut, error) {
			if _, err := t.engagement(in.Slug); err != nil {
				return nil, movedOut{}, err
			}
			e, changed, err := t.s.SetEngagementStatus(in.Slug, in.Status)
			return nil, movedOut{Engagement: &e, Changed: changed}, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "log_interaction", Description: "Log a call, meeting, email or note against a client, with any follow-ups. The record get_today is built from.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in logIn) (*mcp.CallToolResult, store.LogEntry, error) {
			if _, err := t.client(in.Client); err != nil {
				return nil, store.LogEntry{}, err
			}
			if in.Engagement != "" {
				if _, err := t.engagement(in.Engagement); err != nil {
					return nil, store.LogEntry{}, err
				}
			}
			if in.Summary == "" {
				return nil, store.LogEntry{}, errors.New("a log entry needs a summary")
			}
			nl := store.NewLog{Kind: in.Kind, Client: in.Client, Engagement: in.Engagement, Summary: in.Summary, Date: in.Date, With: in.With}
			for _, f := range in.FollowUps {
				nl.FollowUps = append(nl.FollowUps, store.NewFollowUp{Text: f.Text, Due: f.Due})
			}
			l, err := t.s.AddLog(nl)
			return nil, l, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "complete_follow_up", Description: "Tick off a follow-up, named by its log entry and exact text.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in completeIn) (*mcp.CallToolResult, store.FollowUp, error) {
			f, err := t.s.CompleteFollowUpExact(in.Log, in.Text)
			return nil, f, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "log_time", Description: "Log time against an engagement, in days or hours. Warns when that month is already invoiced, since the time will not be on it.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in timeEntryIn) (*mcp.CallToolResult, timeLogged, error) {
			if _, err := t.engagement(in.Engagement); err != nil {
				return nil, timeLogged{}, err
			}
			e, err := t.s.AddTime(store.NewTime{Engagement: in.Engagement, Duration: in.Duration, What: in.What, Date: in.Date})
			if err != nil {
				return nil, timeLogged{}, err
			}
			out := timeLogged{Entry: e}
			if by, err := t.s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
				out.Warning = e.Engagement + " for " + e.Date[:7] + " is already on " + by + "; this time is not on it"
			}
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "change_rate", Description: "Change an engagement's rate from a date on, only when the human tells you their rate changed. The old rate is kept until the day before, so earlier work keeps its value.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in rateIn) (*mcp.CallToolResult, store.Engagement, error) {
			if _, err := t.engagement(in.Engagement); err != nil {
				return nil, store.Engagement{}, err
			}
			e, err := t.s.ChangeRate(in.Engagement, in.Rate, in.From)
			return nil, e, err
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "log_delivery", Description: "Record items delivered on item work (an engagement with basis item, paid per article, video and so on). Day and hourly work takes log_time instead. Warns when that month is already invoiced.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in deliveryIn) (*mcp.CallToolResult, timeLogged, error) {
			if _, err := t.engagement(in.Engagement); err != nil {
				return nil, timeLogged{}, err
			}
			e, err := t.s.AddDelivery(store.NewDelivery{Engagement: in.Engagement, Items: in.Items, What: in.What, Date: in.Date})
			if err != nil {
				return nil, timeLogged{}, err
			}
			out := timeLogged{Entry: e}
			if by, err := t.s.InvoiceCovering(e.Engagement, e.Date[:7]); err == nil && by != "" {
				out.Warning = e.Engagement + " for " + e.Date[:7] + " is already on " + by + "; this is not on it"
			}
			return nil, out, nil
		})
}

type rateIn struct {
	Engagement string `json:"engagement" jsonschema:"exact engagement slug; required"`
	Rate       string `json:"rate" jsonschema:"the new rate, a decimal string; required"`
	From       string `json:"from,omitempty" jsonschema:"the first day at the new rate, YYYY-MM-DD; defaults to today"`
}

type deliveryIn struct {
	Engagement string `json:"engagement" jsonschema:"exact slug of an item engagement; required"`
	Items      string `json:"items,omitempty" jsonschema:"how many were delivered, a decimal string; defaults to 1"`
	What       string `json:"what,omitempty" jsonschema:"what was delivered, e.g. the article's title"`
	Date       string `json:"date,omitempty" jsonschema:"YYYY-MM-DD, today or yesterday; defaults to today"`
}

type timeLogged struct {
	Entry   store.TimeEntry `json:"entry"`
	Warning string          `json:"warning,omitempty"`
}
