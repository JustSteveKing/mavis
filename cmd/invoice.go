package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/pdf"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/JustSteveKing/mavis/internal/ubl"
	"github.com/spf13/cobra"
)

func newInvoiceCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "invoice",
		Aliases: []string{"invoices", "inv"},
		Short:   "Draft, list and show invoices",
	}
	cmd.AddCommand(
		newInvoiceNewCommand(a),
		newInvoiceListCommand(a),
		newInvoiceShowCommand(a),
		newInvoiceEditCommand(a),
		newInvoiceDiscardCommand(a),
		newInvoiceIssueCommand(a),
		newInvoicePDFCommand(a),
		newInvoiceUBLCommand(a),
		newInvoiceCreditCommand(a),
		newInvoicePaidCommand(a, true),
		newInvoicePaidCommand(a, false),
	)
	return cmd
}

func newInvoiceNewCommand(a *app) *cobra.Command {
	var month string
	var lines []string
	cmd := &cobra.Command{
		Use:   "new <client>",
		Short: "Draft an invoice",
		Long: `Drafts invoices/draft-<client>[-<month>].md.

With --month, adds one line per engagement: day and hourly work from that
month's timesheets at the engagement's rate, and a retainer's monthly rate if
it ran that month. An engagement another invoice already covers for that
month is skipped and named, never billed twice. Fixed-price work is never
billed from time: add the milestone with --line.

--line is "description=price", or "description=qty x price unit":

  --line "Rebuild: design milestone=4000"
  --line "Workshop=2 x 500 day"

The draft is a table you can edit by hand; Amount is always recalculated
from Qty and Price. Nothing is numbered until the invoice is issued.`,
		Example: `  mavis invoice new acme --month 2026-10
  mavis invoice new initech --line "Rebuild: design milestone=4000"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			in := store.NewInvoice{Client: args[0], Month: month}
			for _, l := range lines {
				ml, err := parseLineFlag(l)
				if err != nil {
					return err
				}
				in.Lines = append(in.Lines, ml)
			}
			inv, skipped, err := s.AddInvoice(in)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(struct {
					store.Invoice
					Skipped []store.Skipped `json:"skipped"`
				}{inv, skipped})
			}
			a.printf("Drafted %s\n\n", inv.Slug)
			a.invoiceBody(inv)
			if len(skipped) > 0 {
				a.printf("\nLeft off\n")
				for _, sk := range skipped {
					a.printf("  %s: %s\n", sk.Engagement, sk.Reason)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&month, "month", "", "bill this month's time and retainers, YYYY-MM")
	cmd.Flags().StringArrayVar(&lines, "line", nil, `a line by hand, "description=price" or "description=qty x price unit"; repeatable`)
	return cmd
}

// parseLineFlag reads "Workshop=500" or "Workshop=2 x 500 day".
func parseLineFlag(v string) (store.ManualLine, error) {
	desc, rest, ok := strings.Cut(v, "=")
	desc, rest = strings.TrimSpace(desc), strings.TrimSpace(rest)
	if !ok || desc == "" || rest == "" {
		return store.ManualLine{}, fmt.Errorf(`--line %q: use "description=price" or "description=qty x price unit"`, v)
	}
	ml := store.ManualLine{Description: desc}
	if qty, price, ok := strings.Cut(rest, " x "); ok {
		ml.Qty = strings.TrimSpace(qty)
		fields := strings.Fields(price)
		if len(fields) == 0 {
			return ml, fmt.Errorf("--line %q: no price after x", v)
		}
		ml.Price = fields[0]
		ml.Unit = strings.Join(fields[1:], " ")
		return ml, nil
	}
	ml.Price = rest
	return ml, nil
}

func newInvoiceListCommand(a *app) *cobra.Command {
	var client, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List invoices",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			if client != "" {
				c, err := s.ResolveClient(client)
				if err != nil {
					return err
				}
				client = c.Slug
			}
			all, problems, err := s.Invoices()
			if err != nil {
				return err
			}
			a.warn(problems)
			shown := []store.Invoice{}
			for _, inv := range all {
				if (client == "" || inv.Client == client) && (status == "" || inv.Status == status) {
					shown = append(shown, inv)
				}
			}
			if a.jsonOut {
				return a.emitJSON(shown)
			}
			if len(shown) == 0 {
				a.printf("No invoices.\n")
				return nil
			}
			rows := [][]string{{"INVOICE", "STATUS", "CLIENT", "PERIOD", "ISSUED", "DUE", "TOTAL", "OWED"}}
			for _, inv := range shown {
				name := inv.Number
				if name == "" {
					name = inv.Slug
				}
				total := inv.Total.Display() + " " + inv.Currency
				owed := ""
				if inv.Kind == "credit" {
					total = "-" + total
					name += " (" + inv.Credits + ")"
				} else if inv.Balance > 0 {
					owed = inv.Balance.Display()
				}
				rows = append(rows, []string{name, inv.Display(), inv.Client, inv.Period, inv.Issued, inv.Due, total, owed})
			}
			table(a.out, "", map[int]bool{6: true, 7: true}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "only this client's")
	cmd.Flags().StringVar(&status, "status", "", "only invoices with this status: "+strings.Join(store.InvoiceStatuses, ", "))
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(store.InvoiceStatuses, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newInvoiceShowCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show <invoice>",
		Short: "Show an invoice",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.ResolveInvoice(args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(inv)
			}
			title := inv.Number
			if title == "" {
				title = "Draft " + inv.Slug
			}
			if inv.Kind == "credit" {
				title = "Credit note " + strings.TrimPrefix(title, "Draft ") + " against " + inv.Credits
			}
			a.printf("%s, %s, %s\n", title, inv.Client, inv.Display())
			var facts []string
			if inv.Period != "" {
				if t, err := time.Parse("2006-01", inv.Period); err == nil {
					facts = append(facts, t.Format("January 2006"))
				}
			}
			facts = append(facts, inv.Currency, "VAT "+inv.VATTreatment)
			if inv.Issued != "" {
				facts = append(facts, "issued "+inv.Issued)
			}
			if inv.Due != "" {
				facts = append(facts, "due "+inv.Due)
			}
			if inv.Paid != "" {
				facts = append(facts, "paid "+inv.Paid)
			}
			a.printf("%s\n\n", strings.Join(facts, " · "))
			a.invoiceBody(inv)
			if inv.Credited > 0 {
				a.printf("\n  Credited %s; %s still owed\n", inv.Credited.Display(), inv.Balance.Display())
			}
			return nil
		},
	}
}

// invoiceBody prints the lines and the totals beneath them.
func (a *app) invoiceBody(inv store.Invoice) {
	a.linesBody(inv.Lines, inv.VATLines, inv.Net, inv.Total, inv.Currency)
}

func (a *app) linesBody(lines []store.Line, vatLines []store.VATLine, net, total money.Pence, currency string) {
	rows := [][]string{{"DESCRIPTION", "QTY", "UNIT", "PRICE", "VAT", "AMOUNT"}}
	for _, l := range lines {
		rows = append(rows, []string{l.Description, l.Qty, l.Unit, l.Price.Display(), vatLabel(l.VAT), l.Amount.Display()})
	}
	rows = append(rows, []string{"", "", "", "", "Net", net.Display()})
	for _, v := range vatLines {
		if len(vatLines) > 1 || v.Rate > 0 {
			rows = append(rows, []string{"", "", "", "", "VAT " + vatLabel(v.Rate), v.VAT.Display()})
		}
	}
	rows = append(rows, []string{"", "", "", "", "Total", total.Display() + " " + currency})
	table(a.out, "  ", map[int]bool{1: true, 3: true, 4: true, 5: true}, rows)
}

func vatLabel(bp int) string {
	s := strings.TrimRight(strings.TrimRight(money.Pence(bp).String(), "0"), ".")
	return s + "%"
}

func newInvoiceEditCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "edit <draft>",
		Short: "Open a draft in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.ResolveInvoice(args[0])
			if err != nil {
				return err
			}
			if inv.Status != "draft" {
				return fmt.Errorf("%s is %s, and issued invoices do not change; correct it with a credit note", inv.Number, inv.Status)
			}
			if !interactive() {
				a.printf("%s\n", inv.Path)
				return nil
			}
			if err := openEditor(inv.Path); err != nil {
				return err
			}
			// Show what the edit amounts to, or why it does not read.
			again, err := s.ResolveInvoice(inv.Slug)
			if err != nil {
				fmt.Fprintf(a.err, "warning: %v\n", err)
				return nil
			}
			a.invoiceBody(again)
			return nil
		},
	}
}

func newInvoiceDiscardCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "discard <draft>",
		Short: "Delete a draft",
		Long:  "Deletes a draft invoice. Issued invoices are never deleted: they are corrected with a credit note.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.DiscardDraft(args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(inv)
			}
			a.printf("Discarded %s\n", inv.Slug)
			return nil
		},
	}
}

func newInvoiceIssueCommand(a *app) *cobra.Command {
	var date, taxPoint string
	cmd := &cobra.Command{
		Use:   "issue <draft>",
		Short: "Number a draft and freeze it",
		Long: `Gives a draft the next number for the year (INV-2026-001, INV-2026-002,
and from January INV-2027-001), stamps the issue date, tax point and due
date, and renames it invoices/<number>.md.

Everything a VAT invoice must show is checked first: your name, address and
VAT number from the config, and the client's address. Anything missing is
listed in one go, with how to fill it in.

Your details and the client's are copied into the invoice, so it reads the
same however either changes later. After this the invoice does not change,
apart from being marked paid; mistakes are corrected with a credit note.`,
		Example: `  mavis invoice issue draft-acme-2026-10
  mavis invoice issue acme-2026-10 --date 2026-10-31`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			b := a.cfg.Business
			inv, err := s.IssueInvoice(args[0], store.IssueOptions{
				Issuer:            store.Issuer{Name: b.Name, Address: b.Address, Country: b.Country, VATNumber: b.VATNumber, Email: b.Email, PeppolID: b.PeppolID},
				ReverseChargeNote: a.cfg.Invoicing.ReverseChargeNote,
				Date:              date,
				TaxPoint:          taxPoint,
			})
			if err != nil {
				return err
			}
			path, pdfErr := pdf.Write(s.Root(), inv)
			// Setting your own Peppol ID is how you opt in to e-invoices;
			// until then, issuing says nothing about them.
			var xmlPath string
			var missing []ubl.Missing
			var xmlErr error
			einvoicing := a.cfg.Business.PeppolID != ""
			if einvoicing {
				xmlPath, missing, xmlErr = a.writeUBL(s, inv)
			}
			if a.jsonOut {
				return a.emitJSON(struct {
					store.Invoice
					PDF     string        `json:"pdf,omitempty"`
					UBL     string        `json:"ubl,omitempty"`
					Missing []ubl.Missing `json:"ubl_missing,omitempty"`
				}{inv, path, xmlPath, missing})
			}
			if inv.Kind == "credit" {
				a.printf("Issued %s to %s, crediting %s: %s %s\n", inv.Number, inv.ToName, inv.Credits, inv.Total.Display(), inv.Currency)
			} else {
				a.printf("Issued %s to %s: %s %s, due %s\n", inv.Number, inv.ToName, inv.Total.Display(), inv.Currency, inv.Due)
			}
			if pdfErr != nil {
				return fmt.Errorf("%s is issued, but its PDF failed: %w; try mavis invoice pdf %s", inv.Number, pdfErr, inv.Number)
			}
			a.printf("%s\n", path)
			if einvoicing {
				a.reportUBL(inv, xmlPath, missing)
			}
			if xmlErr != nil {
				return fmt.Errorf("%s is issued, but its e-invoice failed: %w; try mavis invoice ubl %s", inv.Number, xmlErr, inv.Number)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "issue date, YYYY-MM-DD, today or yesterday (default: today)")
	cmd.Flags().StringVar(&taxPoint, "tax-point", "", "tax point if it differs from the issue date")
	return cmd
}

func newInvoicePaidCommand(a *app, paid bool) *cobra.Command {
	var date string
	use, short := "paid <invoice>", "Mark an invoice paid"
	if !paid {
		use, short = "unpaid <invoice>", "Take back a paid mark made by mistake"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.SetPaid(args[0], paid, date)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(inv)
			}
			if paid {
				a.printf("%s paid on %s\n", inv.Number, inv.Paid)
			} else {
				a.printf("%s is unpaid again\n", inv.Number)
			}
			return nil
		},
	}
	if paid {
		cmd.Flags().StringVar(&date, "date", "", "when it was paid, YYYY-MM-DD, today or yesterday (default: today)")
	}
	return cmd
}

func newInvoicePDFCommand(a *app) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "pdf <invoice>",
		Short: "Write an invoice's PDF",
		Long: `Writes the invoice to .invoices/<number>.pdf under the records directory,
or to --out. issue does this already; use this to regenerate one, or to
preview a draft, which is marked DRAFT INVOICE and carries no number.

An issued invoice is drawn from the details copied into it at issue, so it
comes out the same however the config or the client has changed since.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.ResolveInvoice(args[0])
			if err != nil {
				return err
			}
			var path string
			if out != "" {
				data, err := pdf.Render(inv, pdf.Options{})
				if err != nil {
					return err
				}
				path = out
				err = os.WriteFile(out, data, 0o644)
				if err != nil {
					return err
				}
			} else if path, err = pdf.Write(s.Root(), inv); err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(map[string]string{"invoice": args[0], "pdf": path})
			}
			a.printf("%s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "write here instead")
	return cmd
}

func newInvoiceCreditCommand(a *app) *cobra.Command {
	var full bool
	var lines []string
	cmd := &cobra.Command{
		Use:   "credit <invoice>",
		Short: "Draft a credit note against an issued invoice",
		Long: `Issued invoices never change. To correct one, draft a credit note against
it: --full credits every line, --line credits part of it. Issue the credit
note like an invoice; it is numbered in its own series, CN-2026-001.

A credit note can never take more than is left on the invoice, checked when
it is drafted and again when it is issued. An invoice's balance, what is
still owed, is its total less its issued credit notes.

A fully credited invoice stops covering its month, so the month can be
billed again, correctly. A part-credited one still covers it.`,
		Example: `  mavis invoice credit INV-2026-001 --full
  mavis invoice credit INV-2026-001 --line "Disputed day=1 x 650 day"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			var manual []store.ManualLine
			for _, l := range lines {
				ml, err := parseLineFlag(l)
				if err != nil {
					return err
				}
				manual = append(manual, ml)
			}
			cn, err := s.AddCreditNote(args[0], full, manual)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(cn)
			}
			a.printf("Drafted %s against %s\n\n", cn.Slug, cn.Credits)
			a.invoiceBody(cn)
			a.printf("\nIssue it with: mavis invoice issue %s\n", cn.Slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "credit every line of the invoice")
	cmd.Flags().StringArrayVar(&lines, "line", nil, `credit part: "description=price" or "description=qty x price unit"; repeatable`)
	return cmd
}

// writeUBL writes an issued invoice's e-invoice beside its PDF when it has
// everything Peppol needs, and otherwise says what it lacks. An invoice is
// never held back for want of an e-invoice: they are not yet mandatory.
func (a *app) writeUBL(s *store.Store, inv store.Invoice) (string, []ubl.Missing, error) {
	if missing := ubl.Check(inv); len(missing) > 0 {
		return "", missing, nil
	}
	var preceding *store.Invoice
	if inv.Kind == "credit" {
		if p, err := s.ResolveInvoice(inv.Credits); err == nil {
			preceding = &p
		}
	}
	data, err := ubl.Write(inv, preceding)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(s.Root(), pdf.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, inv.Number+".xml")
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return "", nil, err
	}
	return path, nil, os.Rename(path+".tmp", path)
}

func (a *app) reportUBL(inv store.Invoice, path string, missing []ubl.Missing) {
	if path != "" {
		a.printf("%s\n", path)
		return
	}
	a.printf("\nNo e-invoice yet; Peppol needs:\n")
	for _, m := range missing {
		a.printf("  %s\n", m)
	}
	a.printf("Then: mavis invoice ubl %s\n", inv.Number)
}

func newInvoiceUBLCommand(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ubl <invoice>",
		Short: "Write an issued invoice as a Peppol e-invoice",
		Long: `Writes .invoices/<number>.xml: Peppol BIS Billing 3.0, UBL 2.1, for an
issued invoice or credit note. Once business.peppol_id is set in the
config, issue does this itself whenever it can; until then, e-invoices are
only made when asked for here.

A document Peppol would reject is not written. Instead this lists what is
missing, each with the rule it breaks and how to fill it: your Peppol ID
(business.peppol_id), the client's (client set --peppol-id), and a buyer
reference (client set --buyer-reference). Peppol IDs are scheme:value, such
as 9932:GB123456789 for a UK VAT number, and are never guessed from a VAT
number: 9932:GB123456789 and 9932:123456789 are different participants.

Identifiers come from the invoice where it recorded them at issue. For one
issued before they were set, the current config and client are used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openStore()
			if err != nil {
				return err
			}
			inv, err := s.ResolveInvoice(args[0])
			if err != nil {
				return err
			}
			// Identifiers, not legal content: fill gaps in an older invoice
			// from what is set now.
			if c, err := s.ResolveClient(inv.Client); err == nil {
				if inv.ToPeppolID == "" {
					inv.ToPeppolID = c.PeppolID
				}
				if inv.BuyerReference == "" {
					inv.BuyerReference = c.BuyerReference
				}
				if inv.ToCountry == "" && c.Country != "" {
					inv.ToCountry = c.Country
				}
			}
			if inv.From.PeppolID == "" {
				inv.From.PeppolID = a.cfg.Business.PeppolID
			}
			if inv.From.Country == "" {
				inv.From.Country = a.cfg.Business.Country
			}
			path, missing, err := a.writeUBL(s, inv)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return a.emitJSON(struct {
					UBL     string        `json:"ubl,omitempty"`
					Missing []ubl.Missing `json:"missing,omitempty"`
				}{path, missing})
			}
			if len(missing) > 0 {
				a.reportUBL(inv, "", missing)
				return fmt.Errorf("%s is not ready to send as an e-invoice", inv.Number)
			}
			a.printf("%s\n", path)
			return nil
		},
	}
}
