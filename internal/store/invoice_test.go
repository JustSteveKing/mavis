package store

import (
	"os"
	"strings"
	"testing"
)

func billingStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme", Name: "Acme Ltd"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "reporting", Title: "Reporting module", Basis: "day", Rate: "650"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "fixes", Title: "Bug fixes", Basis: "hourly", Rate: "90"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "support", Title: "Support", Basis: "retainer", Rate: "1500", Start: "2026-09-01"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "rebuild", Title: "Rebuild", Basis: "fixed", Budget: "12000"})
	for _, e := range []NewTime{
		{Engagement: "reporting", Duration: "1d", Date: "2026-10-06"},
		{Engagement: "reporting", Duration: "1d", Date: "2026-10-07"},
		{Engagement: "reporting", Duration: "1h30m", Date: "2026-10-08"},
		{Engagement: "fixes", Duration: "2h45m", Date: "2026-10-09"},
		{Engagement: "rebuild", Duration: "2d", Date: "2026-10-10"},
		{Engagement: "reporting", Duration: "1d", Date: "2026-09-30"},
	} {
		if _, err := s.AddTime(e); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestDraftFromAMonthOfTime(t *testing.T) {
	s := billingStore(t)
	inv, skipped, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10", Lines: []ManualLine{{Description: "Rebuild: design milestone", Price: "4000"}}})
	if err != nil {
		t.Fatal(err)
	}

	data := string(mustRead(t, inv.Path))
	want := `---
type: invoice
status: draft
client: '[[acme]]'
currency: GBP
vat_treatment: standard
period: 2026-10
engagements: ['[[acme-fixes]]', '[[acme-reporting]]', '[[acme-support]]']
created: 2026-10-01
---
| Description | Qty | Unit | Price | VAT | Amount |
|-------------|-----|------|-------|-----|--------|
| Bug fixes, October 2026 | 2.75 | hour | 90.00 | 20% | 247.50 |
| Reporting module, October 2026 | 2.2 | day | 650.00 | 20% | 1,430.00 |
| Support, October 2026 | 1 | month | 1500.00 | 20% | 1,500.00 |
| Rebuild: design milestone | 1 |  | 4000.00 | 20% | 4,000.00 |
`
	if data != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	if inv.Net != 717750 || inv.VAT != 143550 || inv.Total != 861300 {
		t.Errorf("net %s vat %s total %s", inv.Net, inv.VAT, inv.Total)
	}
	if len(skipped) != 1 || skipped[0].Engagement != "acme-rebuild" {
		t.Errorf("skipped = %+v", skipped)
	}
	if inv.Slug != "draft-acme-2026-10" {
		t.Errorf("slug = %s", inv.Slug)
	}
}

func TestAMonthIsNeverBilledTwice(t *testing.T) {
	s := billingStore(t)
	s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})

	_, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if err == nil || !strings.Contains(err.Error(), "nothing to bill") {
		t.Fatalf("second draft for the same month: %v", err)
	}

	// September has its own time, so it bills, and the retainer's September.
	inv, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-09"})
	if err != nil || len(inv.Lines) != 2 {
		t.Fatalf("september: %+v %v", inv.Lines, err)
	}

	if by, _ := s.InvoiceCovering("acme-reporting", "2026-10"); by != "draft-acme-2026-10" {
		t.Errorf("covering = %q", by)
	}
}

func TestOverseasClientsAreReverseCharged(t *testing.T) {
	s := billingStore(t)
	de := "DE"
	s.SetClient("acme", ClientUpdate{Country: &de})
	inv, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.VATTreatment != "reverse-charge" || inv.VAT != 0 || inv.Lines[0].VAT != 0 {
		t.Fatalf("got %+v", inv)
	}
}

func TestHandEditedDraftsRecalculate(t *testing.T) {
	s := billingStore(t)
	inv, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Workshop", Price: "500"}}})

	// Change the quantity by hand, leave Amount stale, add a zero-rated line.
	data := string(mustRead(t, inv.Path))
	data = strings.Replace(data, "| Workshop | 1 |", "| Workshop | 2 |", 1)
	data += "| Train fare | 1 |  | 84.50 | 0% | |\n"
	os.WriteFile(inv.Path, []byte(data), 0o644)

	got, err := s.ResolveInvoice(inv.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Net != 108450 || got.VAT != 20000 || len(got.VATLines) != 2 {
		t.Fatalf("net %s vat %s lines %+v", got.Net, got.VAT, got.VATLines)
	}
}

func TestABrokenLineIsAnErrorNotADroppedLine(t *testing.T) {
	s := billingStore(t)
	inv, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Workshop", Price: "500"}}})
	os.WriteFile(inv.Path, append(mustRead(t, inv.Path), []byte("| Extra | lots | | 10 | | |\n")...), 0o644)

	invoices, problems, _ := s.Invoices()
	if len(invoices) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Error(), "line 12:") {
		t.Fatalf("invoices %d, problems %v", len(invoices), problems)
	}
}

func TestDiscardOnlyDrafts(t *testing.T) {
	s := billingStore(t)
	inv, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if _, err := s.DiscardDraft(inv.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inv.Path); !os.IsNotExist(err) {
		t.Fatal("draft still there")
	}
	// Once discarded, the month can be billed again.
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"}); err != nil {
		t.Fatal(err)
	}
}

func TestNothingToBill(t *testing.T) {
	s := billingStore(t)
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme"}); err == nil {
		t.Error("no month and no lines should fail")
	}
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2025-01"}); err == nil {
		t.Error("a month with nothing in it should fail")
	}
}

func TestQuantityReadsAsEnglish(t *testing.T) {
	for _, c := range [][3]string{{"1", "day", "1 day"}, {"10", "day", "10 days"}, {"2.75", "hour", "2.75 hours"}, {"3", "seats", "3 seats"}, {"2", "licence", "2 licences"}, {"4", "article", "4 articles"}, {"2", "copy", "2 copies"}, {"3", "day", "3 days"}, {"2", "batch", "2 batches"}, {"1", "article", "1 article"}, {"4", "", "4"}} {
		if got := (Line{Qty: c[0], Unit: c[1]}).Quantity(); got != c[2] {
			t.Errorf("Quantity(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
