package store

import (
	"strings"
	"testing"
)

func issuedFor(t *testing.T, s *Store, month string) Invoice {
	t.Helper()
	d, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: month})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := s.IssueInvoice(d.Slug, IssueOptions{Issuer: me, Date: "2026-10-31"})
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestFullCreditCancelsAndFreesTheMonth(t *testing.T) {
	s := issuable(t)
	inv := issuedFor(t, s, "2026-10")

	cn, err := s.AddCreditNote(inv.Number, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cn.Kind != "credit" || cn.Credits != inv.Number || cn.Total != inv.Total || cn.Slug != "draft-credit-inv-2026-001" {
		t.Fatalf("credit draft: %+v", cn)
	}

	// A draft credit note credits nothing yet.
	still, _ := s.ResolveInvoice(inv.Number)
	if still.Balance != inv.Total {
		t.Fatalf("balance before the credit note is issued: %s", still.Balance)
	}

	issued, err := s.IssueInvoice(cn.Slug, IssueOptions{Issuer: me, Date: "2026-11-02"})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Number != "CN-2026-001" || issued.Due != "" {
		t.Fatalf("issued credit: %+v", issued)
	}
	after, _ := s.ResolveInvoice(inv.Number)
	if after.Balance != 0 || !after.FullyCredited() || after.Display() != "credited" {
		t.Fatalf("after: balance %s, display %s", after.Balance, after.Display())
	}

	// October can be billed again, correctly this time.
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"}); err != nil {
		t.Fatalf("a fully credited month should be billable again: %v", err)
	}
	if _, err := s.AddCreditNote(inv.Number, true, nil); err == nil || !strings.Contains(err.Error(), "already credited in full") {
		t.Fatalf("crediting twice: %v", err)
	}
}

func TestPartialCreditsCannotExceedWhatIsLeft(t *testing.T) {
	s := issuable(t)
	inv := issuedFor(t, s, "2026-10") // 3,813.00 with VAT

	cn, err := s.AddCreditNote(inv.Number, false, []ManualLine{{Description: "Disputed day", Qty: "1", Unit: "day", Price: "650"}})
	if err != nil {
		t.Fatal(err)
	}
	if cn.Total != 78000 { // 650 + 20%
		t.Fatalf("credit total %s", cn.Total)
	}
	s.IssueInvoice(cn.Slug, IssueOptions{Issuer: me})

	after, _ := s.ResolveInvoice(inv.Number)
	if after.Balance != inv.Total-78000 || after.FullyCredited() {
		t.Fatalf("balance %s", after.Balance)
	}
	// October is still covered: only part of it was credited.
	if by, _ := s.InvoiceCovering("acme-reporting", "2026-10"); by != inv.Number {
		t.Fatalf("covering = %q", by)
	}

	if _, err := s.AddCreditNote(inv.Number, false, []ManualLine{{Description: "Too much", Price: "5000"}}); err == nil || !strings.Contains(err.Error(), "only 3,033.00 is left") {
		t.Fatalf("over-credit: %v", err)
	}
	if _, err := s.AddCreditNote(inv.Number, true, nil); err == nil || !strings.Contains(err.Error(), "partly credited") {
		t.Fatalf("--full after a partial credit: %v", err)
	}
}

func TestOverCreditingIsCheckedAgainAtIssue(t *testing.T) {
	s := issuable(t)
	inv := issuedFor(t, s, "2026-10")
	a, _ := s.AddCreditNote(inv.Number, true, nil)
	b, _ := s.AddCreditNote(inv.Number, true, nil) // both drafts fit on their own
	if _, err := s.IssueInvoice(a.Slug, IssueOptions{Issuer: me}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IssueInvoice(b.Slug, IssueOptions{Issuer: me}); err == nil || !strings.Contains(err.Error(), "only 0.00 is left") {
		t.Fatalf("second full credit at issue: %v", err)
	}
}

func TestCreditNotesInStatsAndToday(t *testing.T) {
	s := issuable(t)
	d, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Work", Price: "1000"}}})
	inv, _ := s.IssueInvoice(d.Slug, IssueOptions{Issuer: me, Date: "2026-08-01"}) // 1,200.00, overdue
	cn, _ := s.AddCreditNote(inv.Number, false, []ManualLine{{Description: "Discount", Price: "250"}})
	s.IssueInvoice(cn.Slug, IssueOptions{Issuer: me, Date: "2026-10-01"})

	if _, err := s.SetPaid("CN-2026-001", true, ""); err == nil {
		t.Error("a credit note cannot be paid")
	}

	p, _ := MonthPeriod("2026-10")
	st, _, _ := s.Stats(p)
	if len(st.Invoicing.Credited) != 1 || st.Invoicing.Credited[0].Total != 30000 {
		t.Fatalf("credited: %+v", st.Invoicing.Credited)
	}
	if st.Invoicing.Outstanding[0].Total != 90000 || st.Invoicing.Overdue[0].Total != 90000 {
		t.Fatalf("outstanding should be the balance: %+v", st.Invoicing)
	}
	today, _, _ := s.Today(defaultQuiet)
	if len(today.Unpaid) != 1 || today.Unpaid[0].Balance != 90000 {
		t.Fatalf("today: %+v", today.Unpaid)
	}
}

func TestCreditNeedsAnIssuedInvoice(t *testing.T) {
	s := issuable(t)
	d, _, _ := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if _, err := s.AddCreditNote(d.Slug, true, nil); err == nil {
		t.Error("a draft cannot be credited")
	}
	inv := issuedFor(t, s, "2026-09")
	if _, err := s.AddCreditNote(inv.Number, false, nil); err == nil {
		t.Error("neither --full nor lines should fail")
	}
}
