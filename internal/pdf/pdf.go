// Package pdf renders an invoice as a PDF.
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

// Dir is where PDFs go, under the records root. Dot-prefixed so Obsidian
// does not index binaries; see the vault's conventions.
const Dir = ".invoices"

var grey = &props.Color{Red: 110, Green: 110, Blue: 110}

// Options are for tests: compression off leaves the text searchable.
type Options struct {
	Uncompressed bool
}

// Render draws an invoice. A draft is marked DRAFT and carries no number,
// so a preview can never be mistaken for the real thing.
func Render(inv store.Invoice, o Options) ([]byte, error) {
	b := config.NewBuilder().
		WithLeftMargin(18).
		WithRightMargin(18).
		WithTopMargin(18).
		WithPageNumber(props.PageNumber{Pattern: "Page {current} of {total}", Place: props.RightBottom, Size: 8, Color: grey})
	if !o.Uncompressed {
		b = b.WithCompression(true)
	}
	m := maroto.New(b.Build())

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

	// Who it is from, top right; what it is, top left.
	from := []string{}
	if inv.From.Name != "" {
		from = append(from, inv.From.Address...)
		if inv.From.VATNumber != "" {
			from = append(from, "VAT "+inv.From.VATNumber)
		}
		if inv.From.Email != "" {
			from = append(from, inv.From.Email)
		}
	}
	m.AddRows(row.New(12).Add(
		text.NewCol(6, title, props.Text{Size: 20, Style: fontstyle.Bold}),
		text.NewCol(6, inv.From.Name, props.Text{Size: 11, Style: fontstyle.Bold, Align: align.Right}),
	))
	m.AddRows(row.New(5).Add(
		text.NewCol(6, ref, props.Text{Size: 10, Color: grey}),
		col.New(6),
	))
	m.AddRows(linesCol(from)...)
	m.AddRows(row.New(8))

	// Billed to, and the dates.
	to := []string{inv.ToName}
	if inv.ToName == "" {
		to = []string{inv.Client}
	}
	to = append(to, inv.ToAddress...)
	if inv.ToVATNumber != "" {
		to = append(to, "VAT "+inv.ToVATNumber)
	}
	dates := [][2]string{}
	for _, d := range []struct{ label, value string }{
		{"Invoice date", inv.Issued}, {"Tax point", inv.TaxPoint}, {"Due", inv.Due}, {"Period", period(inv.Period)},
	} {
		if d.value != "" {
			dates = append(dates, [2]string{d.label, long(d.value)})
		}
	}
	m.AddRows(row.New(5).Add(
		text.NewCol(6, "BILL TO", props.Text{Size: 8, Style: fontstyle.Bold, Color: grey}),
		col.New(6),
	))
	n := max(len(to), len(dates))
	for i := 0; i < n; i++ {
		left, label, value := "", "", ""
		if i < len(to) {
			left = to[i]
		}
		if i < len(dates) {
			label, value = dates[i][0], dates[i][1]
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
	for _, l := range inv.Lines {
		m.AddAutoRow(
			text.NewCol(5, l.Description, cell),
			text.NewCol(2, strings.TrimSpace(l.Qty+" "+l.Unit), cellR),
			text.NewCol(2, l.Price.Display(), cellR),
			text.NewCol(1, vat(l.VAT), cellR),
			text.NewCol(2, l.Amount.Display(), cellR),
		)
	}
	m.AddRows(row.New(2), rule())

	// Totals, right-aligned under Amount.
	sym := symbol(inv.Currency)
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
	m.AddRows(total("Net", sym+inv.Net.Display(), false))
	for _, v := range inv.VATLines {
		if v.Rate > 0 || len(inv.VATLines) > 1 {
			m.AddRows(total("VAT at "+vat(v.Rate), sym+v.VAT.Display(), false))
		}
	}
	if inv.VAT == 0 && len(inv.VATLines) <= 1 {
		m.AddRows(total("VAT", sym+money.Pence(0).Display(), false))
	}
	m.AddRows(total("Total "+inv.Currency, sym+inv.Total.Display(), true))

	// The small print.
	var notes []string
	if inv.VATNote != "" {
		notes = append(notes, inv.VATNote)
	}
	if inv.Kind == "credit" {
		notes = append(notes, "This credit note reduces the amount owed on invoice "+inv.Credits+".")
	} else if inv.Due != "" {
		notes = append(notes, "Payment is due by "+long(inv.Due)+".")
	}
	if len(notes) > 0 {
		m.AddRows(row.New(12))
		for _, n := range notes {
			m.AddRows(text.NewAutoRow(n, props.Text{Size: 9, Color: grey}))
		}
	}

	doc, err := m.Generate()
	if err != nil {
		return nil, err
	}
	return doc.GetBytes(), nil
}

// Write renders an invoice into Dir under root and returns the path.
func Write(root string, inv store.Invoice) (string, error) {
	data, err := Render(inv, Options{})
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, Name(inv))
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

// Name is the file name an invoice's PDF is written under.
func Name(inv store.Invoice) string {
	if inv.Number != "" {
		return inv.Number + ".pdf"
	}
	return inv.Slug + ".pdf"
}
