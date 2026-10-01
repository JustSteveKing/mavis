// Package pdf renders invoices, credit notes and quotes as PDFs.
//
// An issued invoice is drawn entirely from its own snapshot (from_*, to_*,
// vat_note), never from the config or the client's note, so a PDF
// regenerated years later says what the original said. Text goes through the
// PDF core fonts, which cover Windows-1252: £, € and accented Latin letters
// render, scripts beyond that do not.
package pdf

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/line"
	"github.com/johnfercher/maroto/v2/pkg/components/row"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

var grey = &props.Color{Red: 110, Green: 110, Blue: 110}

// Options are for tests: compression off leaves the text searchable.
type Options struct {
	Uncompressed bool
}

// Doc is what a page shows, whichever kind of document it is. Invoices,
// credit notes and quotes each map onto it; the layout exists once.
type Doc struct {
	Title, Ref string
	FromName   string
	From       []string // address, VAT number, email
	ToLabel    string   // BILL TO, or PREPARED FOR on a quote
	To         []string // first line is the name
	Dates      [][2]string
	Scope      string // prose above the lines, for quotes
	Lines      []store.Line
	VATLines   []store.VATLine
	Net, VAT   money.Pence
	Total      money.Pence
	Currency   string
	Notes      []string
}

func from(i store.Issuer) []string {
	out := []string{}
	if i.Name == "" {
		return out
	}
	out = append(out, i.Address...)
	if i.VATNumber != "" {
		out = append(out, "VAT "+i.VATNumber)
	}
	if i.Email != "" {
		out = append(out, i.Email)
	}
	return out
}

func to(name, fallback string, address []string, vatNumber string) []string {
	if name == "" {
		name = fallback
	}
	out := append([]string{name}, address...)
	if vatNumber != "" {
		out = append(out, "VAT "+vatNumber)
	}
	return out
}

func dates(pairs ...[2]string) [][2]string {
	var out [][2]string
	for _, p := range pairs {
		if p[1] != "" {
			out = append(out, [2]string{p[0], p[1]})
		}
	}
	return out
}

// FromInvoice lays out an invoice or a credit note. A draft is marked DRAFT
// and carries no number, so a preview can never be mistaken for the real
// thing.
func FromInvoice(inv store.Invoice) Doc {
	title, ref := "INVOICE", inv.Number
	if inv.Kind == "credit" {
		title, ref = "CREDIT NOTE", inv.Number+", crediting invoice "+inv.Credits
	}
	if inv.Status == "draft" {
		title, ref = "DRAFT "+title, "not yet issued"
		if inv.Kind == "credit" {
			ref += ", crediting invoice " + inv.Credits
		}
	}
	d := Doc{
		Title: title, Ref: ref,
		FromName: inv.From.Name, From: from(inv.From),
		ToLabel: "BILL TO", To: to(inv.ToName, inv.Client, inv.ToAddress, inv.ToVATNumber),
		Dates: dates(
			[2]string{"Invoice date", long(inv.Issued)}, [2]string{"Tax point", long(inv.TaxPoint)},
			[2]string{"Due", long(inv.Due)}, [2]string{"Period", period(inv.Period)},
		),
		Lines: inv.Lines, VATLines: inv.VATLines, Net: inv.Net, VAT: inv.VAT, Total: inv.Total, Currency: inv.Currency,
	}
	if inv.VATNote != "" {
		d.Notes = append(d.Notes, inv.VATNote)
	}
	if inv.Kind == "credit" {
		d.Notes = append(d.Notes, "This credit note reduces the amount owed on invoice "+inv.Credits+".")
	} else if inv.Due != "" {
		d.Notes = append(d.Notes, "Payment is due by "+long(inv.Due)+".")
	}
	return d
}

// FromQuote lays out a quote: its scope above the lines, and how long it
// stands below them.
func FromQuote(q store.Quote) Doc {
	ref := q.Number + ": " + q.Title
	title := "QUOTE"
	if q.Status == "draft" {
		title, ref = "DRAFT QUOTE", q.Title+", not yet sent"
	}
	d := Doc{
		Title: title, Ref: ref,
		FromName: q.From.Name, From: from(q.From),
		ToLabel: "PREPARED FOR", To: to(q.ToName, q.Client, q.ToAddress, q.ToVATNumber),
		Dates: dates([2]string{"Date", long(q.Sent)}, [2]string{"Valid until", long(q.ValidUntil)}),
		Scope: q.Scope,
		Lines: q.Lines, VATLines: q.VATLines, Net: q.Net, VAT: q.VAT, Total: q.Total, Currency: q.Currency,
	}
	if q.VATNote != "" {
		d.Notes = append(d.Notes, q.VATNote)
	}
	if q.ValidUntil != "" {
		d.Notes = append(d.Notes, "This quote is valid until "+long(q.ValidUntil)+".")
	}
	return d
}

// Render draws an invoice or credit note.
func Render(inv store.Invoice, o Options) ([]byte, error) { return RenderDoc(FromInvoice(inv), o) }

// RenderDoc draws any document.
func RenderDoc(doc Doc, o Options) ([]byte, error) {
	b := config.NewBuilder().
		WithLeftMargin(18).
		WithRightMargin(18).
		WithTopMargin(18).
		WithPageNumber(props.PageNumber{Pattern: "Page {current} of {total}", Place: props.RightBottom, Size: 8, Color: grey})
	if !o.Uncompressed {
		b = b.WithCompression(true)
	}
	m := maroto.New(b.Build())

	// What it is, top left; who it is from, top right.
	m.AddRows(row.New(12).Add(
		text.NewCol(6, doc.Title, props.Text{Size: 20, Style: fontstyle.Bold}),
		text.NewCol(6, doc.FromName, props.Text{Size: 11, Style: fontstyle.Bold, Align: align.Right}),
	))
	m.AddRows(row.New(5).Add(
		text.NewCol(6, doc.Ref, props.Text{Size: 10, Color: grey}),
		col.New(6),
	))
	m.AddRows(linesCol(doc.From)...)
	m.AddRows(row.New(8))

	// Who it is for, and the dates.
	m.AddRows(row.New(5).Add(
		text.NewCol(6, doc.ToLabel, props.Text{Size: 8, Style: fontstyle.Bold, Color: grey}),
		col.New(6),
	))
	n := max(len(doc.To), len(doc.Dates))
	for i := 0; i < n; i++ {
		left, label, value := "", "", ""
		if i < len(doc.To) {
			left = doc.To[i]
		}
		if i < len(doc.Dates) {
			label, value = doc.Dates[i][0], doc.Dates[i][1]
		}
		style := fontstyle.Normal
		if i == 0 {
			style = fontstyle.Bold
		}
		m.AddRows(row.New(5).Add(
			text.NewCol(6, left, props.Text{Size: 10, Style: style}),
			text.NewCol(3, label, props.Text{Size: 9, Color: grey, Align: align.Right}),
			text.NewCol(3, value, props.Text{Size: 10, Align: align.Right}),
		))
	}
	m.AddRows(row.New(10))

	// Scope, paragraph by paragraph.
	if doc.Scope != "" {
		for _, para := range strings.Split(doc.Scope, "\n\n") {
			para = strings.Join(strings.Fields(para), " ")
			if para != "" {
				m.AddRows(text.NewAutoRow(para, props.Text{Size: 10}))
				m.AddRows(row.New(3))
			}
		}
		m.AddRows(row.New(5))
	}

	// The lines.
	head := props.Text{Size: 8, Style: fontstyle.Bold, Color: grey}
	headR := head
	headR.Align = align.Right
	m.AddRows(row.New(6).Add(
		text.NewCol(5, "DESCRIPTION", head),
		text.NewCol(2, "QUANTITY", headR),
		text.NewCol(2, "PRICE", headR),
		text.NewCol(1, "VAT", headR),
		text.NewCol(2, "AMOUNT", headR),
	))
	m.AddRows(rule())
	cell := props.Text{Size: 9, Top: 1.5}
	cellR := cell
	cellR.Align = align.Right
	for _, l := range doc.Lines {
		m.AddAutoRow(
			text.NewCol(5, l.Description, cell),
			text.NewCol(2, l.Quantity(), cellR),
			text.NewCol(2, l.Price.Display(), cellR),
			text.NewCol(1, vat(l.VAT), cellR),
			text.NewCol(2, l.Amount.Display(), cellR),
		)
	}
	m.AddRows(row.New(2), rule())

	// Totals, right-aligned under Amount.
	sym := symbol(doc.Currency)
	total := func(label, value string, bold bool) core.Row {
		st := fontstyle.Normal
		if bold {
			st = fontstyle.Bold
		}
		return row.New(6).Add(
			col.New(7),
			text.NewCol(3, label, props.Text{Size: 10, Style: st, Align: align.Right, Top: 1}),
			text.NewCol(2, value, props.Text{Size: 10, Style: st, Align: align.Right, Top: 1}),
		)
	}
	m.AddRows(total("Net", sym+doc.Net.Display(), false))
	for _, v := range doc.VATLines {
		if v.Rate > 0 || len(doc.VATLines) > 1 {
			m.AddRows(total("VAT at "+vat(v.Rate), sym+v.VAT.Display(), false))
		}
	}
	if doc.VAT == 0 && len(doc.VATLines) <= 1 {
		m.AddRows(total("VAT", sym+money.Pence(0).Display(), false))
	}
	m.AddRows(total("Total "+doc.Currency, sym+doc.Total.Display(), true))

	// The small print.
	if len(doc.Notes) > 0 {
		m.AddRows(row.New(12))
		for _, n := range doc.Notes {
			m.AddRows(text.NewAutoRow(n, props.Text{Size: 9, Color: grey}))
		}
	}

	out, err := m.Generate()
	if err != nil {
		return nil, err
	}
	return out.GetBytes(), nil
}

// Write renders an invoice into dir, the store's files folder, and returns
// the path.
func Write(dir string, inv store.Invoice) (string, error) {
	return WriteDoc(dir, FromInvoice(inv), Name(inv.Number, inv.Slug))
}

// WriteQuote renders a quote into dir and returns the path.
func WriteQuote(dir string, q store.Quote) (string, error) {
	return WriteDoc(dir, FromQuote(q), Name(q.Number, q.Slug))
}

// WriteDoc renders a document to dir/name, replacing any older copy in one
// rename.
func WriteDoc(dir string, doc Doc, name string) (string, error) {
	data, err := RenderDoc(doc, Options{})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

func linesCol(lines []string) []core.Row {
	var rows []core.Row
	for _, l := range lines {
		rows = append(rows, row.New(4.5).Add(
			col.New(6),
			text.NewCol(6, l, props.Text{Size: 9, Align: align.Right, Color: grey}),
		))
	}
	return rows
}

func rule() core.Row {
	return row.New(1).Add(col.New(12).Add(line.New(props.Line{Color: grey, Thickness: 0.2})))
}

func vat(bp int) string {
	return strings.TrimRight(strings.TrimRight(money.Pence(bp).String(), "0"), ".") + "%"
}

func symbol(currency string) string {
	switch currency {
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "USD":
		return "$"
	}
	return ""
}

func long(date string) string {
	if date == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("2 January 2006")
}

func period(month string) string {
	if month == "" {
		return ""
	}
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return t.Format("January 2006")
}

// Name is the file name a document's PDF is written under: its number once
// it has one, its slug before.
func Name(number, slug string) string {
	if number != "" {
		return number + ".pdf"
	}
	return slug + ".pdf"
}
