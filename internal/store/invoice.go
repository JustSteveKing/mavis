package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/record"
)

// InvoiceStatuses: a draft can be edited and discarded; an issued invoice
// is frozen apart from being marked paid.
var InvoiceStatuses = []string{"draft", "issued", "paid"}

// standardVAT is the UK standard rate in basis points.
const standardVAT = 2000

type Line struct {
	Description string      `json:"description"`
	Qty         string      `json:"qty"`
	Unit        string      `json:"unit,omitempty"`
	Price       money.Pence `json:"price_pence"`
	VAT         int         `json:"vat_bp"`       // basis points: 2000 is 20%
	Amount      money.Pence `json:"amount_pence"` // always Qty times Price
}

type VATLine struct {
	Rate int         `json:"rate_bp"`
	Net  money.Pence `json:"net_pence"`
	VAT  money.Pence `json:"vat_pence"`
}

type Invoice struct {
	Slug         string   `json:"slug"`
	Number       string   `json:"number,omitempty"`
	Kind         string   `json:"kind"`              // invoice or credit
	Credits      string   `json:"credits,omitempty"` // for a credit note, the invoice it corrects
	Status       string   `json:"status"`
	Client       string   `json:"client"`
	Currency     string   `json:"currency"`
	VATTreatment string   `json:"vat_treatment"`
	Period       string   `json:"period,omitempty"` // YYYY-MM, for invoices built from time
	Engagements  []string `json:"engagements,omitempty"`
	Created      string   `json:"created,omitempty"`
	Issued       string   `json:"issued,omitempty"`
	TaxPoint     string   `json:"tax_point,omitempty"`
	Due          string   `json:"due,omitempty"`
	Paid         string   `json:"paid,omitempty"`

	// Copied in at issue, so the invoice reads the same whatever changes
	// later in the config or the client's note.
	From        Issuer   `json:"from"`
	ToName      string   `json:"to_name,omitempty"`
	ToAddress   []string `json:"to_address,omitempty"`
	ToVATNumber string   `json:"to_vat_number,omitempty"`
	ToCountry   string   `json:"to_country,omitempty"`
	ToPeppolID  string   `json:"to_peppol_id,omitempty"`

	// BuyerReference is the client's reference for the invoice, which
	// Peppol requires (PEPPOL-EN16931-R003). Set on a draft, or copied from
	// the client at issue.
	BuyerReference string `json:"buyer_reference,omitempty"`
	VATNote        string `json:"vat_note,omitempty"`

	Lines    []Line      `json:"lines"`
	VATLines []VATLine   `json:"vat_lines"`
	Net      money.Pence `json:"net_pence"`
	VAT      money.Pence `json:"vat_pence"`
	Total    money.Pence `json:"total_pence"`

	// Derived from issued credit notes, never stored. Balance is what is
	// still owed: nothing for a draft, a paid invoice or a credit note.
	Credited money.Pence `json:"credited_pence"`
	Balance  money.Pence `json:"balance_pence"`

	Path string `json:"path"`
}

// FullyCredited reports an issued invoice whose credit notes cancel it.
func (inv Invoice) FullyCredited() bool {
	return inv.Kind == "invoice" && inv.Status != "draft" && inv.Credited >= inv.Total
}

// Display is the status to show: an issued invoice credited to nothing reads
// as credited.
func (inv Invoice) Display() string {
	if inv.Status == "issued" && inv.FullyCredited() {
		return "credited"
	}
	return inv.Status
}

const linesHeader = "| Description | Qty | Unit | Price | VAT | Amount |\n|-------------|-----|------|-------|-----|--------|\n"

// parseLines reads the lines table. A row that will not parse is an error
// naming its line: an invoice must not quietly drop a line the way a
// listing may skip a bad timesheet row.
func parseLines(d *record.Document, defaultVAT int) ([]Line, error) {
	body := d.Body
	var lines []Line
	for i, row := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(row), "|") {
			continue
		}
		c := cells(row)
		if strings.EqualFold(c[0], "description") {
			continue
		}
		if !slices.ContainsFunc(c, func(s string) bool { return !separatorCell.MatchString(s) }) {
			continue
		}
		for len(c) < 6 {
			c = append(c, "")
		}
		if c[0] == "" {
			return nil, fmt.Errorf("line %d: a line needs a description", d.LineOf(i))
		}
		qty, err := money.Parse(c[1])
		if err != nil || qty <= 0 {
			return nil, fmt.Errorf("line %d: quantity %q is not a number above zero", d.LineOf(i), c[1])
		}
		price, err := money.Parse(c[3])
		if err != nil {
			return nil, fmt.Errorf("line %d: price %q is not an amount", d.LineOf(i), c[3])
		}
		vat, err := parseVAT(c[4], defaultVAT)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", d.LineOf(i), err)
		}
		lines = append(lines, Line{
			Description: c[0],
			Qty:         trimQty(qty),
			Unit:        c[2],
			Price:       price,
			VAT:         vat,
			Amount:      price.MulDiv(int64(qty), 100),
		})
	}
	return lines, nil
}

// parseVAT reads 20%, 0% or 17.5% into basis points. Blank takes the
// invoice's default.
func parseVAT(s string, def int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	bp, err := money.Parse(strings.TrimSuffix(s, "%"))
	if err != nil || bp < 0 || bp > 10000 {
		return 0, fmt.Errorf("VAT %q: use a percentage like 20%%", s)
	}
	return int(bp), nil
}

func vatCell(bp int) string { return trimQty(money.Pence(bp)) + "%" }

// trimQty renders hundredths without trailing zeros: 5, 2.75, 0.5.
func trimQty(q money.Pence) string {
	s := strings.TrimRight(strings.TrimRight(q.String(), "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func renderLines(lines []Line) string {
	var b strings.Builder
	b.WriteString(linesHeader)
	for _, l := range lines {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			strings.ReplaceAll(l.Description, "|", `\|`), l.Qty, l.Unit, l.Price.String(), vatCell(l.VAT), l.Amount.Display())
	}
	return b.String()
}

// totals works VAT out per rate, rounding once per rate rather than per
// line, as HMRC allows.
func totals(lines []Line) ([]VATLine, money.Pence, money.Pence) {
	byRate := map[int]money.Pence{}
	for _, l := range lines {
		byRate[l.VAT] += l.Amount
	}
	rates := make([]int, 0, len(byRate))
	for r := range byRate {
		rates = append(rates, r)
	}
	slices.Sort(rates)
	slices.Reverse(rates)

	var out []VATLine
	var net, vat money.Pence
	for _, r := range rates {
		v := byRate[r].MulDiv(int64(r), 10000)
		out = append(out, VATLine{Rate: r, Net: byRate[r], VAT: v})
		net += byRate[r]
		vat += v
	}
	return out, net, vat
}

func defaultVATFor(treatment string) int {
	if treatment == "standard" {
		return standardVAT
	}
	return 0
}

func invoiceFrom(path string, d *record.Document) (Invoice, error) {
	inv := Invoice{
		Slug:         strings.TrimSuffix(filepath.Base(path), ".md"),
		Number:       d.Get("number"),
		Kind:         d.Get("kind"),
		Credits:      linkTarget(d.Get("credits")),
		Status:       d.Get("status"),
		Client:       linkTarget(d.Get("client")),
		Currency:     d.Get("currency"),
		VATTreatment: d.Get("vat_treatment"),
		Period:       d.Get("period"),
		Created:      d.Get("created"),
		Issued:       d.Get("issued"),
		TaxPoint:     d.Get("tax_point"),
		Due:          d.Get("due"),
		Paid:         d.Get("paid"),
		From: Issuer{
			Name:      d.Get("from_name"),
			Address:   d.List("from_address"),
			VATNumber: d.Get("from_vat_number"),
			Email:     d.Get("from_email"),
			Country:   d.Get("from_country"),
			PeppolID:  d.Get("from_peppol_id"),
		},
		ToName:         d.Get("to_name"),
		ToAddress:      d.List("to_address"),
		ToVATNumber:    d.Get("to_vat_number"),
		ToCountry:      d.Get("to_country"),
		ToPeppolID:     d.Get("to_peppol_id"),
		BuyerReference: d.Get("buyer_reference"),
		VATNote:        d.Get("vat_note"),
		Path:           path,
	}
	for _, e := range d.List("engagements") {
		inv.Engagements = append(inv.Engagements, linkTarget(e))
	}
	if inv.Currency == "" {
		inv.Currency = "GBP"
	}
	if inv.Kind == "" {
		inv.Kind = "invoice"
	}
	if inv.VATTreatment == "" {
		inv.VATTreatment = "standard"
	}
	lines, err := parseLines(d, defaultVATFor(inv.VATTreatment))
	if err != nil {
		return inv, err
	}
	inv.Lines = lines
	inv.VATLines, inv.Net, inv.VAT = totals(lines)
	inv.Total = inv.Net + inv.VAT
	if err := checkFrozen(inv, d.Get("net"), d.Get("vat"), d.Get("total")); err != nil {
		return inv, err
	}
	return inv, nil
}

// Invoices lists every invoice, numbered ones in number order, then drafts.
func (s *Store) Invoices() ([]Invoice, []Problem, error) {
	dir := filepath.Join(s.root, s.layout.Invoices)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var out []Invoice
	var problems []Problem
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		d, err := read(path)
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		if d.Get("type") != "invoice" {
			continue
		}
		inv, err := invoiceFrom(path, d)
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		out = append(out, inv)
	}
	settle(out)
	slices.SortStableFunc(out, func(a, b Invoice) int {
		switch {
		case a.Number == "" && b.Number != "":
			return 1
		case a.Number != "" && b.Number == "":
			return -1
		}
		if c := strings.Compare(a.Number, b.Number); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out, problems, nil
}

// ResolveInvoice finds an invoice by number, slug, or a unique substring of
// either.
func (s *Store) ResolveInvoice(query string) (Invoice, error) {
	all, _, err := s.Invoices()
	if err != nil {
		return Invoice{}, err
	}
	for _, inv := range all {
		if inv.Number == query {
			return inv, nil
		}
	}
	return resolve("invoice", query, all,
		func(i Invoice) string { return i.Slug },
		func(i Invoice) string { return i.Number })
}

// ManualLine is a line given by hand: fixed-price milestones, expenses,
// anything not built from time.
type ManualLine struct {
	Description, Qty, Unit, Price string
}

type NewInvoice struct {
	Client string
	Month  string // YYYY-MM: build lines from that month's time and retainers
	Lines  []ManualLine

	// Engagement limits a month's lines to one engagement, by exact slug.
	// Retainer drafts use it to bill the retainer and nothing else.
	Engagement string
}

// Skipped is an engagement left off a draft, and why.
type Skipped struct {
	Engagement string `json:"engagement"`
	Reason     string `json:"reason"`
}

// AddInvoice writes a draft. With a month, it adds one line per engagement:
// day and hourly work from that month's timesheet at the engagement's rate,
// and a retainer's monthly rate if it ran that month. An engagement already
// covered for that month by another invoice is skipped, never billed twice.
func (s *Store) AddInvoice(in NewInvoice) (Invoice, []Skipped, error) {
	var monthLabel string
	if in.Month != "" {
		t, err := time.Parse(monthLayout, in.Month)
		if err != nil {
			return Invoice{}, nil, fmt.Errorf("month %q: use YYYY-MM", in.Month)
		}
		monthLabel = t.Format("January 2006")
	} else if len(in.Lines) == 0 {
		return Invoice{}, nil, errors.New("give a --month to bill time, or at least one --line")
	}

	var out Invoice
	var skipped []Skipped
	err := s.withLock(func() error {
		client, err := s.ResolveClient(in.Client)
		if err != nil {
			return err
		}
		if client.InvoicedElsewhere() {
			return client.errInvoicedElsewhere()
		}
		vat := defaultVATFor(client.Treatment())

		var lines []Line
		var covered []string
		if in.Month != "" {
			lines, covered, skipped, err = s.linesForMonth(client, in.Month, monthLabel, vat, in.Engagement)
			if err != nil {
				return err
			}
		}
		for _, m := range in.Lines {
			l, err := manualLine(m, vat)
			if err != nil {
				return err
			}
			lines = append(lines, l)
		}
		if len(lines) == 0 {
			return fmt.Errorf("nothing to bill %s for %s", client.Slug, monthLabel)
		}

		dir := filepath.Join(s.root, s.layout.Invoices)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		name := "draft-" + client.Slug
		if in.Engagement != "" {
			name = "draft-" + in.Engagement
		}
		if in.Month != "" {
			name += "-" + in.Month
		}
		path, err := freePath(dir, name)
		if err != nil {
			return err
		}

		d := record.New()
		d.Set("type", "invoice")
		d.Set("status", "draft")
		d.Set("client", link(client.Slug))
		d.Set("currency", client.Currency)
		d.Set("vat_treatment", client.Treatment())
		if in.Month != "" {
			d.Set("period", in.Month)
		}
		if len(covered) > 0 {
			links := make([]string, len(covered))
			for i, e := range covered {
				links[i] = link(e)
			}
			d.SetList("engagements", links)
		}
		d.SetPlain("created", s.today())
		s.stamp(d)
		d.Body = renderLines(lines)

		if err := write(path, d); err != nil {
			return err
		}
		out, err = invoiceFrom(path, d)
		return err
	})
	return out, skipped, err
}

// linesForMonth builds one line per engagement of the client for a month.
func (s *Store) linesForMonth(client Client, month, label string, vat int, only string) ([]Line, []string, []Skipped, error) {
	engagements, _, err := s.Engagements()
	if err != nil {
		return nil, nil, nil, err
	}
	entries, _, err := s.TimeEntries()
	if err != nil {
		return nil, nil, nil, err
	}
	invoices, _, err := s.Invoices()
	if err != nil {
		return nil, nil, nil, err
	}
	billed := map[string]string{}
	for _, inv := range invoices {
		if inv.Period == month && !inv.FullyCredited() {
			for _, e := range inv.Engagements {
				billed[e] = inv.Slug
				if inv.Number != "" {
					billed[e] = inv.Number
				}
			}
		}
	}
	minutes := map[string]int{}
	items := map[string]money.Pence{}
	worked := map[string][]TimeEntry{}
	for _, e := range entries {
		if strings.HasPrefix(e.Date, month) {
			minutes[e.Engagement] += e.Minutes
			items[e.Engagement] += e.Count
			worked[e.Engagement] = append(worked[e.Engagement], e)
		}
	}
	p, _ := MonthPeriod(month)

	var lines []Line
	var covered []string
	var skipped []Skipped
	for _, e := range engagements {
		if e.Client != client.Slug || (only != "" && e.Slug != only) {
			continue
		}
		hasWork := minutes[e.Slug] > 0 || items[e.Slug] > 0
		isRetainer := e.Basis == "retainer" && retainerMonths(e, p) > 0
		if !hasWork && !isRetainer {
			continue
		}
		if by, ok := billed[e.Slug]; ok {
			skipped = append(skipped, Skipped{e.Slug, "already on " + by})
			continue
		}
		if _, err := money.Parse(e.Rate); e.Rate == "" || err != nil {
			skipped = append(skipped, Skipped{e.Slug, "no rate set"})
			continue
		}
		if e.rateErr != nil {
			skipped = append(skipped, Skipped{e.Slug, e.rateErr.Error()})
			continue
		}

		// One line per rate: a month that spans a rate change bills each
		// part at the rate on its days.
		var add []Line
		line := func(qty money.Pence, unit string, rate money.Pence) {
			add = append(add, Line{
				Description: e.Title + ", " + label,
				Qty:         trimQty(qty),
				Unit:        unit,
				Price:       rate,
				VAT:         vat,
				Amount:      rate.MulDiv(int64(qty), 100),
			})
		}
		switch e.Basis {
		case "day", "hourly", "item":
			if e.Basis == "item" && items[e.Slug] == 0 {
				skipped = append(skipped, Skipped{e.Slug, "time logged but nothing delivered"})
				continue
			}
			groups, err := groupByRate(e, worked[e.Slug])
			if err != nil {
				return nil, nil, nil, err
			}
			for _, g := range groups {
				switch e.Basis {
				case "day":
					if g.minutes > 0 {
						line(money.Pence(int64(g.minutes)*100).MulDiv(1, int64(s.DayMinutes)), "day", g.rate)
					}
				case "hourly":
					if g.minutes > 0 {
						line(money.Pence(int64(g.minutes)*100).MulDiv(1, 60), "hour", g.rate)
					}
				case "item":
					if g.count > 0 {
						line(g.count, e.UnitName(), g.rate)
					}
				}
			}
		case "retainer":
			rate, _, err := e.RateOn(month + "-01")
			if err != nil {
				return nil, nil, nil, err
			}
			line(100, "month", rate)
		case "fixed":
			skipped = append(skipped, Skipped{e.Slug, "fixed price: add the milestone with --line"})
			continue
		default:
			skipped = append(skipped, Skipped{e.Slug, "no basis set"})
			continue
		}
		lines = append(lines, add...)
		covered = append(covered, e.Slug)
	}
	return lines, covered, skipped, nil
}

// InvoiceCovering returns the invoice, if any, that bills an engagement for
// a month.
func (s *Store) InvoiceCovering(engagement, month string) (string, error) {
	invoices, _, err := s.Invoices()
	if err != nil {
		return "", err
	}
	for _, inv := range invoices {
		if inv.Period == month && !inv.FullyCredited() && slices.Contains(inv.Engagements, engagement) {
			if inv.Number != "" {
				return inv.Number, nil
			}
			return inv.Slug, nil
		}
	}
	return "", nil
}

// DiscardDraft deletes a draft. Issued invoices cannot be discarded: they are
// corrected with a credit note.
func (s *Store) DiscardDraft(query string) (Invoice, error) {
	var out Invoice
	err := s.withLock(func() error {
		inv, err := s.ResolveInvoice(query)
		if err != nil {
			return err
		}
		if inv.Status != "draft" {
			return fmt.Errorf("%s is %s; an issued invoice is corrected with a credit note, never deleted", inv.Number, inv.Status)
		}
		out = inv
		return os.Remove(inv.Path)
	})
	return out, err
}

// settle works out each invoice's credits and balance from the issued credit
// notes that reference it.
func settle(all []Invoice) {
	credited := map[string]money.Pence{}
	for _, inv := range all {
		if inv.Kind == "credit" && inv.Status != "draft" {
			credited[inv.Credits] += inv.Total
		}
	}
	for i := range all {
		inv := &all[i]
		if inv.Kind != "invoice" {
			continue
		}
		inv.Credited = credited[inv.Number]
		if inv.Status == "issued" && inv.Total > inv.Credited {
			inv.Balance = inv.Total - inv.Credited
		}
	}
}

// AddCreditNote drafts a credit note against an issued invoice: every line
// with full, or the lines given. It cannot credit more than is left.
func (s *Store) AddCreditNote(query string, full bool, manual []ManualLine) (Invoice, error) {
	if full == (len(manual) > 0) {
		return Invoice{}, errors.New("give --full to credit the whole invoice, or --line for part of it, not both")
	}
	var out Invoice
	err := s.withLock(func() error {
		inv, err := s.ResolveInvoice(query)
		if err != nil {
			return err
		}
		if inv.Kind != "invoice" || inv.Status == "draft" {
			return fmt.Errorf("%s is not an issued invoice; only those take a credit note", query)
		}
		remaining := inv.Total - inv.Credited
		if remaining <= 0 {
			return fmt.Errorf("%s is already credited in full", inv.Number)
		}

		vat := defaultVATFor(inv.VATTreatment)
		var lines []Line
		if full {
			if inv.Credited > 0 {
				return fmt.Errorf("%s is already partly credited (%s); credit the rest with --line", inv.Number, inv.Credited.Display())
			}
			lines = append(lines, inv.Lines...)
		}
		for _, m := range manual {
			qty := m.Qty
			if qty == "" {
				qty = "1"
			}
			q, err := money.Parse(qty)
			if err != nil || q <= 0 {
				return fmt.Errorf("line %q: quantity %q is not a number above zero", m.Description, qty)
			}
			price, err := money.Parse(m.Price)
			if err != nil || price <= 0 {
				return fmt.Errorf("line %q: price %q is not an amount above zero", m.Description, m.Price)
			}
			lines = append(lines, Line{Description: m.Description, Qty: trimQty(q), Unit: m.Unit, Price: price, VAT: vat, Amount: price.MulDiv(int64(q), 100)})
		}
		_, net, tax := totals(lines)
		if net+tax > remaining {
			return fmt.Errorf("that credits %s, but only %s is left on %s", (net + tax).Display(), remaining.Display(), inv.Number)
		}

		path, err := freePath(filepath.Join(s.root, s.layout.Invoices), "draft-credit-"+strings.ToLower(inv.Number))
		if err != nil {
			return err
		}
		d := record.New()
		d.Set("type", "invoice")
		d.Set("kind", "credit")
		d.Set("status", "draft")
		d.Set("credits", link(inv.Number))
		d.Set("client", link(inv.Client))
		d.Set("currency", inv.Currency)
		d.Set("vat_treatment", inv.VATTreatment)
		d.SetPlain("created", s.today())
		s.stamp(d)
		d.Body = renderLines(lines)
		if err := write(path, d); err != nil {
			return err
		}
		out, err = invoiceFrom(path, d)
		return err
	})
	return out, err
}

// Quantity joins a line's quantity and unit for reading: "10 days",
// "1 day", "2.75 hours". Only the units mavis writes itself (day, hour,
// month) are pluralised; anything typed by hand is left as typed.
func (l Line) Quantity() string {
	if l.Unit == "" {
		return l.Qty
	}
	return l.Qty + " " + Plural(l.Unit, l.Qty)
}

// Plural is a unit for a quantity: 1 article, 4 articles, 0.5 days. English
// enough for the units people bill by. A unit already ending in s is left as
// written, since a line typed by hand often says "3 seats" already.
func Plural(unit, qty string) string {
	if qty == "1" || unit == "" || strings.HasSuffix(unit, "s") {
		return unit
	}
	n := len(unit)
	switch {
	case strings.HasSuffix(unit, "x"), strings.HasSuffix(unit, "ch"), strings.HasSuffix(unit, "sh"):
		return unit + "es"
	case n > 1 && unit[n-1] == 'y' && !strings.ContainsRune("aeiou", rune(unit[n-2])):
		return unit[:n-1] + "ies"
	}
	return unit + "s"
}
