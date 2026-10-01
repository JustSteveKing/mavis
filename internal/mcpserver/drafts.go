package mcpserver

import (
	"context"
	"errors"

	"github.com/JustSteveKing/mavis/internal/remind"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type lineIn struct {
	Description string `json:"description" jsonschema:"what the line is for; required"`
	Price       string `json:"price" jsonschema:"unit price as a decimal string, e.g. 650; required"`
	Qty         string `json:"qty,omitempty" jsonschema:"quantity as a decimal string; defaults to 1"`
	Unit        string `json:"unit,omitempty" jsonschema:"day, hour, month, or anything else"`
}

func manual(lines []lineIn) []store.ManualLine {
	out := make([]store.ManualLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, store.ManualLine{Description: l.Description, Qty: l.Qty, Unit: l.Unit, Price: l.Price})
	}
	return out
}

type draftInvoiceIn struct {
	Client string   `json:"client" jsonschema:"exact client slug; required"`
	Month  string   `json:"month,omitempty" jsonschema:"YYYY-MM: bill that month's day and hourly time and retainers"`
	Lines  []lineIn `json:"lines,omitempty" jsonschema:"lines by hand, e.g. a fixed-price milestone"`
}

type draftInvoiceOut struct {
	Draft   store.Invoice   `json:"draft"`
	Skipped []store.Skipped `json:"skipped" jsonschema:"engagements left off, and why"`
	Next    string          `json:"next" jsonschema:"what the human runs to issue it"`
}

type draftsOut struct {
	Drafts []store.Invoice `json:"drafts"`
	Count  int             `json:"count"`
	Next   string          `json:"next"`
}

type creditIn struct {
	Invoice string   `json:"invoice" jsonschema:"exact number of the issued invoice, e.g. INV-2026-001; required"`
	Full    bool     `json:"full,omitempty" jsonschema:"credit every line; or give lines instead"`
	Lines   []lineIn `json:"lines,omitempty" jsonschema:"the part to credit"`
}

type draftQuoteIn struct {
	Client    string   `json:"client" jsonschema:"exact client slug; required"`
	Title     string   `json:"title" jsonschema:"what the quote is for; required"`
	Scope     string   `json:"scope,omitempty" jsonschema:"what the client gets, as prose; printed above the lines"`
	Lines     []lineIn `json:"lines,omitempty"`
	ValidDays int      `json:"valid_days,omitempty" jsonschema:"days it stands once sent; defaults to 30"`
}

type reminderOut struct {
	remind.Reminder
	Invoice string `json:"invoice"`
	Next    string `json:"next" jsonschema:"what the human runs once they have sent it"`
}

func (t *tools) registerDrafts(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "draft_invoice", Description: "Draft an invoice. With month, one line per engagement from that month's time and retainers; an engagement already invoiced for the month is skipped, never billed twice. Fixed-price work goes in lines. The human issues it.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in draftInvoiceIn) (*mcp.CallToolResult, draftInvoiceOut, error) {
			if _, err := t.client(in.Client); err != nil {
				return nil, draftInvoiceOut{}, err
			}
			inv, skipped, err := t.s.AddInvoice(store.NewInvoice{Client: in.Client, Month: in.Month, Lines: manual(in.Lines)})
			if err != nil {
				return nil, draftInvoiceOut{}, err
			}
			if skipped == nil {
				skipped = []store.Skipped{}
			}
			return nil, draftInvoiceOut{Draft: inv, Skipped: skipped, Next: "mavis invoice issue " + inv.Slug}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "draft_retainer_invoices", Description: "Draft one invoice for each retainer month that is due, holding the retainer alone. The human issues them.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, draftsOut, error) {
			due, err := t.s.RetainersDue()
			if err != nil {
				return nil, draftsOut{}, err
			}
			out := draftsOut{Drafts: []store.Invoice{}, Next: "mavis invoice issue <draft>, for each"}
			for _, r := range due {
				inv, err := t.s.DraftRetainer(r.Engagement, r.Month)
				if err != nil {
					return nil, out, err
				}
				out.Drafts = append(out.Drafts, inv)
			}
			out.Count = len(out.Drafts)
			return nil, out, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "draft_credit_note", Description: "Draft a credit note against an issued invoice, all of it or part. It can never exceed what is left. The human issues it.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in creditIn) (*mcp.CallToolResult, draftInvoiceOut, error) {
			inv, err := t.invoice(in.Invoice)
			if err != nil {
				return nil, draftInvoiceOut{}, err
			}
			cn, err := t.s.AddCreditNote(inv.Number, in.Full, manual(in.Lines))
			if err != nil {
				return nil, draftInvoiceOut{}, err
			}
			return nil, draftInvoiceOut{Draft: cn, Skipped: []store.Skipped{}, Next: "mavis invoice issue " + cn.Slug}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "draft_quote", Description: "Draft a quote with a title, a prose scope and lines. The human sends it.", Annotations: writes},
		func(ctx context.Context, req *mcp.CallToolRequest, in draftQuoteIn) (*mcp.CallToolResult, quoteDrafted, error) {
			if _, err := t.client(in.Client); err != nil {
				return nil, quoteDrafted{}, err
			}
			q, err := t.s.AddQuote(store.NewQuote{Client: in.Client, Title: in.Title, Scope: in.Scope, Lines: manual(in.Lines), ValidDays: in.ValidDays})
			if err != nil {
				return nil, quoteDrafted{}, err
			}
			return nil, quoteDrafted{Draft: q, Next: "mavis quote send " + q.Slug}, nil
		})

	mcp.AddTool(srv, &mcp.Tool{Name: "draft_reminder", Description: "Draft the next payment reminder for an overdue invoice: subject and message, worded for how far the chase has got. Nothing is sent or logged; give it to the human.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in refIn) (*mcp.CallToolResult, reminderOut, error) {
			inv, err := t.invoice(in.Ref)
			if err != nil {
				return nil, reminderOut{}, err
			}
			if inv.Number == "" {
				return nil, reminderOut{}, errors.New("a draft has not been issued, so it cannot be overdue")
			}
			sent, err := t.s.Reminders(inv.Number)
			if err != nil {
				return nil, reminderOut{}, err
			}
			var dates []string
			for _, r := range sent {
				dates = append(dates, r.Date[:10])
			}
			contact := ""
			if c, err := t.s.ResolveClient(inv.Client); err == nil {
				contact = c.Contact
			}
			signoff := t.o.Signoff
			if signoff == "" {
				signoff = inv.From.Name
			}
			r, err := remind.Write(remind.Input{
				Invoice: inv, Contact: contact, Signoff: signoff, Earlier: dates,
				Today: t.s.Now().Format("2006-01-02"), Statutory: t.o.Statutory,
			})
			if err != nil {
				return nil, reminderOut{}, err
			}
			return nil, reminderOut{Reminder: r, Invoice: inv.Number, Next: "mavis invoice remind " + inv.Number + " --sent, once it is sent"}, nil
		})
}

type quoteDrafted struct {
	Draft store.Quote `json:"draft"`
	Next  string      `json:"next"`
}
