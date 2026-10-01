package store

import (
	"os"
	"strings"
	"testing"
)

func timeStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "reporting", Basis: "day", Rate: "650"})
	return s
}

func TestAddTimeCreatesThenAppendsToTheMonthsSheet(t *testing.T) {
	s := timeStore(t)
	if _, err := s.AddTime(NewTime{Engagement: "reporting", Duration: "1d", What: "Report filters", Date: "2026-10-06"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTime(NewTime{Engagement: "reporting", Duration: "2h", What: "Call with Jo | scoping", Date: "2026-10-07"}); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(s.SheetPath("acme-reporting", "2026-10"))
	want := `---
type: timesheet
engagement: '[[acme-reporting]]'
client: '[[acme]]'
month: 2026-10
---
| Date | Time | What |
|------|------|------|
| 2026-10-06 | 1d | Report filters |
| 2026-10-07 | 2h | Call with Jo \| scoping |
`
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}

	entries, problems, err := s.TimeEntries()
	if err != nil || len(problems) != 0 {
		t.Fatal(err, problems)
	}
	if len(entries) != 2 || entries[0].Minutes != 450 || entries[1].What != "Call with Jo | scoping" || entries[1].Client != "acme" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestANewMonthStartsANewSheet(t *testing.T) {
	s := timeStore(t)
	s.AddTime(NewTime{Engagement: "reporting", Duration: "1d", Date: "2026-10-31"})
	s.AddTime(NewTime{Engagement: "reporting", Duration: "1d", Date: "2026-11-02"})
	for _, month := range []string{"2026-10", "2026-11"} {
		if _, err := os.Stat(s.SheetPath("acme-reporting", month)); err != nil {
			t.Errorf("%s: %v", month, err)
		}
	}
}

func TestHandEditedSheets(t *testing.T) {
	s := timeStore(t)
	s.AddTime(NewTime{Engagement: "reporting", Duration: "1d", Date: "2026-10-06"})
	path := s.SheetPath("acme-reporting", "2026-10")

	// By hand: a row with a typo, a row added with aligned columns, and a
	// note under the table.
	data, _ := os.ReadFile(path)
	edited := string(data) +
		"| 2026-10-07 | three hours | oops |\n" +
		"|2026-10-08|   0.5d   |   Aligned by an editor   |\n" +
		"\nRemember to ask about the export format.\n"
	os.WriteFile(path, []byte(edited), 0o644)

	entries, problems, _ := s.TimeEntries()
	if len(entries) != 2 || entries[1].Minutes != 225 || entries[1].What != "Aligned by an editor" {
		t.Errorf("entries = %+v", entries)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "line 10:") {
		t.Errorf("problems = %v", problems)
	}

	// A new row goes after the table, not after the note.
	s.AddTime(NewTime{Engagement: "reporting", Duration: "1h", Date: "2026-10-09"})
	data, _ = os.ReadFile(path)
	if !strings.HasSuffix(string(data), "| 2026-10-09 | 1h |  |\n\nRemember to ask about the export format.\n") {
		t.Fatalf("row landed in the wrong place:\n%s", data)
	}
}

func TestAddTimeValidates(t *testing.T) {
	s := timeStore(t)
	for name, in := range map[string]NewTime{
		"bad duration":  {Engagement: "reporting", Duration: "ages"},
		"bad date":      {Engagement: "reporting", Duration: "1h", Date: "last tuesday"},
		"no engagement": {Engagement: "nothing", Duration: "1h"},
	} {
		if _, err := s.AddTime(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseDay(t *testing.T) {
	s := newStore(t)
	for in, want := range map[string]string{"": "2026-10-01", "today": "2026-10-01", "yesterday": "2026-09-30", "2026-09-15": "2026-09-15"} {
		if got, err := s.ParseDay(in); err != nil || got != want {
			t.Errorf("ParseDay(%q) = %q, %v", in, got, err)
		}
	}
}

func TestCells(t *testing.T) {
	got := cells(`| 2026-10-06 | 1d | a \| b |`)
	if len(got) != 3 || got[2] != "a | b" {
		t.Fatalf("cells = %q", got)
	}
}
