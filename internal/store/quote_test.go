package store

import (
	"os"
	"strings"
	"testing"
)

func quoteStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	s.AddClient(NewClient{Slug: "globex", Name: "Globex Corporation", Status: "prospect"})
	return s
}

func draftQuote(t *testing.T, s *Store) Quote {
	t.Helper()
	q, err := s.AddQuote(NewQuote{
		Client: "globex", Title: "Reporting rebuild",
		Scope: "A rebuilt reporting module.\n\nExports to CSV and PDF, and a scheduled email.",
		Lines: []ManualLine{{Description: "Discovery and design", Qty: "3", Unit: "day", Price: "650"}, {Description: "Build", Qty: "10", Unit: "day", Price: "650"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestQuoteDraftHasScopeAboveItsLines(t *testing.T) {
	s := quoteStore(t)
	q := draftQuote(t, s)
	data := string(mustRead(t, q.Path))
	if !strings.Contains(data, "title: Reporting rebuild") || !strings.Contains(data, "valid_days: 30") {
		t.Fatalf("frontmatter:\n%s", data)
	}
	if q.Scope == "" || !strings.HasPrefix(q.Scope, "A rebuilt reporting module.") || len(q.Lines) != 2 || q.Net != 845000 {
		t.Fatalf("parsed %+v", q)
	}
	if _, err := s.AddQuote(NewQuote{Client: "globex"}); err == nil {
		t.Error("a quote without a title should fail")
	}
}

func TestSendNumbersAndFreezesWithoutAnAddress(t *testing.T) {
	s := quoteStore(t)
	q := draftQuote(t, s)

	if _, err := s.SendQuote(q.Slug, IssueOptions{}); err == nil {
		t.Fatal("sending with no name of your own should fail")
	}
	sent, err := s.SendQuote(q.Slug, IssueOptions{Issuer: Issuer{Name: "Steve Ltd"}, Date: "2026-10-01"})
	if err != nil {
		t.Fatalf("a prospect with no address can still be quoted: %v", err)
	}
	if sent.Number != "Q-2026-001" || sent.ValidUntil != "2026-10-31" || sent.ToName != "Globex Corporation" || sent.Scope == "" {
		t.Fatalf("sent %+v", sent)
	}
	if _, err := os.Stat(q.Path); !os.IsNotExist(err) {
		t.Error("the draft should be gone")
	}

	// Edited after sending: caught.
	data := string(mustRead(t, sent.Path))
	os.WriteFile(sent.Path, []byte(strings.Replace(data, "| 10 | day |", "| 12 | day |", 1)), 0o644)
	if _, problems, _ := s.Quotes(); len(problems) != 1 || !strings.Contains(problems[0].Error(), "edited after it was sent") {
		t.Fatalf("problems = %v", problems)
	}
	os.WriteFile(sent.Path, []byte(data), 0o644)

	if _, err := s.DiscardQuote(sent.Number); err == nil {
		t.Error("a sent quote cannot be discarded")
	}
}

func TestAcceptStartsTheEngagement(t *testing.T) {
	s := quoteStore(t)
	q := draftQuote(t, s)
	sent, _ := s.SendQuote(q.Slug, IssueOptions{Issuer: Issuer{Name: "Steve Ltd"}, Date: "2026-10-01"})

	got, eng, err := s.DecideQuote(sent.Number, true, "2026-10-05", &StartWork{Name: "reporting"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "accepted" || got.Decided != "2026-10-05" || got.Engagement != "globex-reporting" {
		t.Fatalf("quote %+v", got)
	}
	if eng.Basis != "fixed" || eng.Budget != "8450.00" || eng.Status != "active" || eng.Start != "2026-10-05" || eng.Title != "Reporting rebuild" {
		t.Fatalf("engagement %+v", eng)
	}
	if !strings.Contains(string(mustRead(t, eng.Path)), "quote: '[[Q-2026-001]]'") {
		t.Error("the engagement should link back to its quote")
	}
	if _, _, err := s.DecideQuote(sent.Number, false, "", nil); err == nil {
		t.Error("answering twice should fail")
	}
}

func TestAFailedEngagementLeavesTheQuoteOpen(t *testing.T) {
	s := quoteStore(t)
	s.AddEngagement(NewEngagement{Client: "globex", Name: "reporting"})
	q := draftQuote(t, s)
	sent, _ := s.SendQuote(q.Slug, IssueOptions{Issuer: Issuer{Name: "Steve Ltd"}})

	if _, _, err := s.DecideQuote(sent.Number, true, "", &StartWork{Name: "reporting"}); err == nil || !strings.Contains(err.Error(), "still open") {
		t.Fatalf("err = %v", err)
	}
	again, _ := s.ResolveQuote(sent.Number)
	if again.Status != "sent" {
		t.Fatalf("the quote should still be open, got %s", again.Status)
	}
	if _, eng, err := s.DecideQuote(sent.Number, true, "", &StartWork{Name: "reporting-rebuild", Basis: "day", Rate: "650"}); err != nil || eng.Basis != "day" || eng.Budget != "" {
		t.Fatalf("retry with a day rate: %+v %v", eng, err)
	}
}

func TestExpiryIsDerived(t *testing.T) {
	s := quoteStore(t)
	q := draftQuote(t, s)
	sent, _ := s.SendQuote(q.Slug, IssueOptions{Issuer: Issuer{Name: "Steve Ltd"}, Date: "2026-08-01"})
	if !sent.Expired("2026-10-01") || sent.Display("2026-10-01") != "expired" || sent.Display("2026-08-15") != "sent" {
		t.Fatalf("expiry: %+v", sent)
	}
	// Late acceptance is still recorded: the client said yes.
	if got, _, err := s.DecideQuote(sent.Number, true, "", nil); err != nil || got.Status != "accepted" {
		t.Fatalf("late accept: %v", err)
	}
}

func TestQuotesInTodayAndStats(t *testing.T) {
	s := quoteStore(t)
	me := Issuer{Name: "Steve Ltd"}
	a := draftQuote(t, s)
	b := draftQuote(t, s)
	c := draftQuote(t, s)
	qa, _ := s.SendQuote(a.Slug, IssueOptions{Issuer: me, Date: "2026-08-01"}) // expired by today
	qb, _ := s.SendQuote(b.Slug, IssueOptions{Issuer: me, Date: "2026-09-20"}) // waiting
	qc, _ := s.SendQuote(c.Slug, IssueOptions{Issuer: me, Date: "2026-09-25"})
	s.DecideQuote(qc.Number, false, "2026-10-01", nil)

	today, _, _ := s.Today(defaultQuiet)
	if len(today.Quotes) != 2 || today.Quotes[0].Number != qa.Number || today.Quotes[1].Number != qb.Number {
		t.Fatalf("today: %+v", today.Quotes)
	}

	p, _ := MonthPeriod("2026-10")
	st, _, _ := s.Stats(p)
	qs := st.Quoting
	if qs == nil || len(qs.Declined) != 1 || qs.Waiting[0].Count != 1 || qs.Expired[0].Count != 1 || len(qs.Sent) != 0 {
		t.Fatalf("stats: %+v", qs)
	}
}
