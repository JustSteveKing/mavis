package store

import (
	"os"
	"strings"
	"testing"
	"time"
)

func retainerStore(t *testing.T) *Store {
	t.Helper()
	s := issuable(t)
	s.AddClient(NewClient{Slug: "globex"})
	s.SetClient("globex", ClientUpdate{Address: []string{"2 Low Road"}})
	s.AddEngagement(NewEngagement{Client: "globex", Name: "care", Title: "Care plan", Basis: "retainer", Rate: "800", Start: "2026-08-15"})
	return s
}

func months(due []RetainerDue) string {
	var out []string
	for _, d := range due {
		out = append(out, d.Engagement+" "+d.Month)
	}
	return strings.Join(out, ", ")
}

func TestRetainersAreDueInArrears(t *testing.T) {
	s := retainerStore(t) // today is 2026-10-01; acme-support started 2026-09-01
	due, err := s.RetainersDue()
	if err != nil {
		t.Fatal(err)
	}
	// September is over; October is not. globex's care plan has August too.
	if got := months(due); got != "globex-care 2026-08, acme-support 2026-09, globex-care 2026-09" {
		t.Fatalf("due = %s", got)
	}
}

func TestDraftRetainerBillsOnlyTheRetainer(t *testing.T) {
	s := retainerStore(t)
	s.AddTime(NewTime{Engagement: "reporting", Duration: "1d", Date: "2026-09-10"}) // other acme work in September

	inv, err := s.DraftRetainer("acme-support", "2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Lines) != 1 || inv.Lines[0].Description != "Support, September 2026" || inv.Net != 150000 || inv.Slug != "draft-acme-support-2026-09" {
		t.Fatalf("draft: %+v", inv)
	}
	due, _ := s.RetainersDue()
	if strings.Contains(months(due), "acme-support 2026-09") {
		t.Fatal("a drafted month is no longer due")
	}
	// September's day-rate work is still there to bill on its own.
	other, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-09"})
	if err != nil || len(other.Lines) != 1 || !strings.HasPrefix(other.Lines[0].Description, "Reporting module") {
		t.Fatalf("the rest of September: %+v %v", other.Lines, err)
	}

	if _, err := s.DraftRetainer("acme-support", "2026-09"); err == nil || !strings.Contains(err.Error(), "already on") {
		t.Fatalf("twice: %v", err)
	}
	if _, err := s.DraftRetainer("reporting", "2026-09"); err == nil {
		t.Fatal("day-rate work is not a retainer")
	}
}

func TestRetainerEdges(t *testing.T) {
	s := retainerStore(t)
	s.SetEngagementStatus("globex-care", "paused")
	if strings.Contains(months(mustDue(t, s)), "globex-care") {
		t.Error("a paused retainer is not due")
	}
	s.SetEngagementStatus("globex-care", "active")

	// Done at the end of August: only August.
	s.Now = func() time.Time { return time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC) }
	s.SetEngagementStatus("globex-care", "done")
	s.Now = func() time.Time { return time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC) }
	if got := months(mustDue(t, s)); strings.Count(got, "globex-care") != 1 || !strings.Contains(got, "globex-care 2026-08") {
		t.Errorf("done retainer: %s", got)
	}

	// Started today: no month has finished yet.
	s.AddEngagement(NewEngagement{Client: "globex", Name: "hosting", Basis: "retainer", Rate: "50"})
	if strings.Contains(months(mustDue(t, s)), "globex-hosting") {
		t.Error("a retainer started today has no finished month")
	}

	// No start date at all, which only a hand edit produces: just the latest
	// finished month, never a guessed backlog.
	e, _ := s.AddEngagement(NewEngagement{Client: "globex", Name: "domains", Basis: "retainer", Rate: "10"})
	data := string(mustRead(t, e.Path))
	os.WriteFile(e.Path, []byte(strings.Replace(data, "start: 2026-12-01\n", "", 1)), 0o644)
	if got := months(mustDue(t, s)); strings.Count(got, "globex-domains") != 1 || !strings.Contains(got, "globex-domains 2026-11") {
		t.Errorf("no start date: %s", got)
	}

	// A fully credited month comes back.
	inv, _ := s.DraftRetainer("acme-support", "2026-09")
	issued, _ := s.IssueInvoice(inv.Slug, IssueOptions{Issuer: me})
	cn, _ := s.AddCreditNote(issued.Number, true, nil)
	s.IssueInvoice(cn.Slug, IssueOptions{Issuer: me})
	if !strings.Contains(months(mustDue(t, s)), "acme-support 2026-09") {
		t.Error("a credited month should be due again")
	}
}

func mustDue(t *testing.T, s *Store) []RetainerDue {
	t.Helper()
	due, err := s.RetainersDue()
	if err != nil {
		t.Fatal(err)
	}
	return due
}
