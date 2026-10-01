package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var me = Issuer{Name: "Steve Ltd", Address: []string{"1 My Street", "Leeds LS1 1AA"}, VATNumber: "GB999999973", Email: "me@example.com"}

func issuable(t *testing.T) *Store {
	t.Helper()
	s := billingStore(t)
	if _, err := s.SetClient("acme", ClientUpdate{Address: []string{"1 High Street", "Manchester M1 1AA"}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIssueNumbersFreezesAndSnapshots(t *testing.T) {
	s := issuable(t)
	draft, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})

	inv, err := s.IssueInvoice(draft.Slug, IssueOptions{Issuer: me, Date: "2026-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number != "INV-2026-001" || inv.Status != "issued" || inv.Due != "2026-11-30" || inv.TaxPoint != "2026-10-31" {
		t.Fatalf("got %+v", inv)
	}
	if filepath.Base(inv.Path) != "INV-2026-001.md" {
		t.Errorf("path = %s", inv.Path)
	}
	if _, err := os.Stat(draft.Path); !os.IsNotExist(err) {
		t.Error("the draft file should be gone")
	}
	data := string(mustRead(t, inv.Path))
	for _, want := range []string{"number: INV-2026-001", `total: "3813.00"`, "from_name: Steve Ltd", "to_name: Acme Ltd", "to_address: [1 High Street, Manchester M1 1AA]", "from_vat_number: GB999999973"} {
		if !strings.Contains(data, want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}

	// The client moves; the issued invoice still says where they were.
	s.SetClient("acme", ClientUpdate{Address: []string{"Somewhere else"}})
	again, _ := s.ResolveInvoice("INV-2026-001")
	if again.ToAddress[0] != "1 High Street" {
		t.Errorf("snapshot changed: %v", again.ToAddress)
	}

	if _, err := s.IssueInvoice("INV-2026-001", IssueOptions{Issuer: me}); err == nil {
		t.Error("issuing twice should fail")
	}
	if _, err := s.DiscardDraft("INV-2026-001"); err == nil {
		t.Error("an issued invoice cannot be discarded")
	}
}

func TestNumbersRunOnAndResetEachYear(t *testing.T) {
	s := issuable(t)
	issue := func(date string) string {
		d, _, err := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Work", Price: "100"}}})
		if err != nil {
			t.Fatal(err)
		}
		inv, err := s.IssueInvoice(d.Slug, IssueOptions{Issuer: me, Date: date})
		if err != nil {
			t.Fatal(err)
		}
		return inv.Number
	}
	got := []string{issue("2026-11-01"), issue("2026-12-01"), issue("2027-01-04"), issue("2026-12-30")}
	want := []string{"INV-2026-001", "INV-2026-002", "INV-2027-001", "INV-2026-003"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestIssueListsEverythingMissingAtOnce(t *testing.T) {
	s := billingStore(t) // acme has no address
	draft, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})

	_, err := s.IssueInvoice(draft.Slug, IssueOptions{})
	var missing *MissingError
	if !errors.As(err, &missing) || len(missing.Missing) != 4 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "mavis client set acme --address") {
		t.Errorf("should say how to fix it: %v", err)
	}
	if _, err := os.Stat(draft.Path); err != nil {
		t.Error("a refused issue must leave the draft alone")
	}
}

func TestReverseChargeCarriesItsNote(t *testing.T) {
	s := issuable(t)
	de := "DE"
	s.SetClient("acme", ClientUpdate{Country: &de})
	draft, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})

	if _, err := s.IssueInvoice(draft.Slug, IssueOptions{Issuer: me}); err == nil {
		t.Fatal("reverse charge without wording should be refused")
	}
	inv, err := s.IssueInvoice(draft.Slug, IssueOptions{Issuer: me, ReverseChargeNote: "Reverse charge applies."})
	if err != nil {
		t.Fatal(err)
	}
	if inv.VAT != 0 || inv.VATNote != "Reverse charge applies." {
		t.Fatalf("got %+v", inv)
	}
}

func TestAnIssuedInvoiceEditedByHandIsCaught(t *testing.T) {
	s := issuable(t)
	draft, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	inv, _ := s.IssueInvoice(draft.Slug, IssueOptions{Issuer: me})

	data := string(mustRead(t, inv.Path))
	os.WriteFile(inv.Path, []byte(strings.Replace(data, "| 650.00 |", "| 700.00 |", 1)), 0o644)

	invoices, problems, _ := s.Invoices()
	if len(invoices) != 0 || len(problems) != 1 || !strings.Contains(problems[0].Error(), "edited after it was issued") {
		t.Fatalf("invoices %d, problems %v", len(invoices), problems)
	}
}

func TestPaid(t *testing.T) {
	s := issuable(t)
	draft, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if _, err := s.SetPaid(draft.Slug, true, ""); err == nil {
		t.Error("a draft cannot be paid")
	}
	inv, _ := s.IssueInvoice(draft.Slug, IssueOptions{Issuer: me, Date: "2026-10-01"})

	if _, err := s.SetPaid(inv.Number, true, "2026-09-01"); err == nil {
		t.Error("paid before it was issued")
	}
	got, err := s.SetPaid(inv.Number, true, "2026-10-01")
	if err != nil || got.Status != "paid" || got.Paid != "2026-10-01" {
		t.Fatalf("paid: %+v %v", got, err)
	}
	if _, err := s.SetPaid(inv.Number, true, ""); err == nil {
		t.Error("paying twice should fail")
	}
	got, err = s.SetPaid(inv.Number, false, "")
	if err != nil || got.Status != "issued" || got.Paid != "" {
		t.Fatalf("unpaid: %+v %v", got, err)
	}
}

func TestInvoicingInStatsAndToday(t *testing.T) {
	s := issuable(t)
	p, _ := MonthPeriod("2026-10")

	st, _, _ := s.Stats(p)
	if st.Invoicing != nil {
		t.Fatal("with nothing issued, invoicing should be absent, not zero")
	}

	a, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "A", Price: "1000"}}})
	b, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "B", Price: "500"}}})
	s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "draft", Price: "999"}}})
	ia, _ := s.IssueInvoice(a.Slug, IssueOptions{Issuer: me, Date: "2026-08-01"}) // due 2026-08-31
	ib, _ := s.IssueInvoice(b.Slug, IssueOptions{Issuer: me, Date: "2026-10-01"})
	s.SetPaid(ib.Number, true, "2026-10-01")

	st, _, _ = s.Stats(p)
	inv := st.Invoicing
	if inv == nil || len(inv.Invoiced) != 1 || inv.Invoiced[0].Total != 60000 || inv.Paid[0].Total != 60000 {
		t.Fatalf("invoiced/paid: %+v", inv)
	}
	if inv.Outstanding[0].Total != 120000 || inv.Overdue[0].Count != 1 {
		t.Fatalf("outstanding/overdue: %+v", inv)
	}

	today, _, _ := s.Today(defaultQuiet)
	if len(today.Unpaid) != 1 || today.Unpaid[0].Number != ia.Number {
		t.Fatalf("today: %+v", today.Unpaid)
	}
}
