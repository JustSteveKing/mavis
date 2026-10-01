package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddLogWritesTheDesignedShape(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "reporting"})
	s.Now = func() time.Time { return time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC) }

	l, err := s.AddLog(NewLog{
		Kind: "call", Client: "acme", Engagement: "reporting",
		Summary: "Scoped the reporting module.",
		With:    []string{"Jo Bloggs"},
		FollowUps: []NewFollowUp{
			{Text: "Send estimate", Due: "+7d"},
			{Text: "Book a follow-up call"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(l.Path)
	want := `---
type: log
kind: call
date: 2026-10-01T14:30
client: '[[acme]]'
engagement: '[[acme-reporting]]'
with: [Jo Bloggs]
---
Scoped the reporting module.

## Follow-ups
- [ ] Send estimate (due 2026-10-08)
- [ ] Book a follow-up call
`
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	if filepath.Base(l.Path) != "2026-10-01-acme-call.md" {
		t.Errorf("path = %s", l.Path)
	}
	if l.Summary != "Scoped the reporting module." || len(l.FollowUps) != 2 || l.FollowUps[0].Due != "2026-10-08" {
		t.Errorf("parsed %+v", l)
	}
}

func TestTwoCallsOnOneDayGetTwoFiles(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	a, _ := s.AddLog(NewLog{Kind: "call", Client: "acme"})
	b, err := s.AddLog(NewLog{Kind: "call", Client: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Path == b.Path || !strings.HasSuffix(b.Path, "-call-2.md") {
		t.Fatalf("%s, %s", a.Path, b.Path)
	}
}

func TestAddLogRefusesAnotherClientsEngagement(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddClient(NewClient{Slug: "globex"})
	s.AddEngagement(NewEngagement{Client: "globex", Name: "audit"})
	if _, err := s.AddLog(NewLog{Kind: "call", Client: "acme", Engagement: "audit"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestFollowUpsFromHandWrittenCheckboxes(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	l, _ := s.AddLog(NewLog{Kind: "note", Client: "acme", Summary: "Notes."})

	// Added by hand in Obsidian, outside any Follow-ups heading.
	data, _ := os.ReadFile(l.Path)
	os.WriteFile(l.Path, append(data, []byte("\nThings to do:\n* [ ] Chase invoice (due 2026-09-30)\n- [x] Already done\n- [ ] Someday\n")...), 0o644)

	open, _, err := s.FollowUps()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 || open[0].Text != "Chase invoice" || open[0].Due != "2026-09-30" || open[1].Due != "" {
		t.Fatalf("open = %+v", open)
	}
}

func TestCompleteFollowUpTicksOnlyThatLine(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddClient(NewClient{Slug: "globex"})
	l, _ := s.AddLog(NewLog{Kind: "call", Client: "acme", Summary: "Call.", FollowUps: []NewFollowUp{{Text: "Send estimate"}, {Text: "Send contract"}}})
	s.AddLog(NewLog{Kind: "call", Client: "globex", FollowUps: []NewFollowUp{{Text: "Send estimate"}}})

	var amb *AmbiguousError
	if _, err := s.CompleteFollowUp("", "estimate"); !errors.As(err, &amb) {
		t.Fatalf("across clients should be ambiguous: %v", err)
	}
	if _, err := s.CompleteFollowUp("acme", "send"); !errors.As(err, &amb) {
		t.Fatalf("two of acme's match: %v", err)
	}

	before, _ := os.ReadFile(l.Path)
	f, err := s.CompleteFollowUp("acme", "estimate")
	if err != nil || !f.Done {
		t.Fatalf("%+v %v", f, err)
	}
	after, _ := os.ReadFile(l.Path)
	if want := strings.Replace(string(before), "- [ ] Send estimate", "- [x] Send estimate", 1); string(after) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", after, want)
	}

	if _, err := s.CompleteFollowUp("acme", "estimate"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a done follow-up is not open: %v", err)
	}
}

func TestFollowUpsSortSoonestFirstUndatedLast(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddLog(NewLog{Kind: "note", Client: "acme", FollowUps: []NewFollowUp{
		{Text: "undated"}, {Text: "later", Due: "2026-12-01"}, {Text: "sooner", Due: "2026-10-02"},
	}})
	open, _, _ := s.FollowUps()
	var got []string
	for _, f := range open {
		got = append(got, f.Text)
	}
	if strings.Join(got, ",") != "sooner,later,undated" {
		t.Fatalf("order = %v", got)
	}
}

func TestParseDue(t *testing.T) {
	s := newStore(t)
	for in, want := range map[string]string{"2026-10-08": "2026-10-08", "+3d": "2026-10-04", "+2w": "2026-10-15"} {
		if got, err := s.ParseDue(in); err != nil || got != want {
			t.Errorf("ParseDue(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := s.ParseDue("friday"); err == nil {
		t.Error("want an error")
	}
}
