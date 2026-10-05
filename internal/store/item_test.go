package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func itemStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	if _, err := s.AddClient(NewClient{Slug: "sevalla", Name: "Sevalla"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEngagement(NewEngagement{Client: "sevalla", Name: "articles", Title: "Articles", Basis: "item", Rate: "750", Unit: " Article "}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEngagement(NewEngagement{Client: "sevalla", Name: "consulting", Basis: "day", Rate: "600"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestItemEngagementKeepsItsUnit(t *testing.T) {
	s := itemStore(t)
	e, err := s.ResolveEngagement("sevalla-articles")
	if err != nil {
		t.Fatal(err)
	}
	if e.Basis != "item" || e.Unit != "article" || e.Rate != "750.00" {
		t.Errorf("got %+v", e)
	}
	if _, err := s.AddEngagement(NewEngagement{Client: "sevalla", Name: "videos", Basis: "day", Unit: "video"}); err == nil {
		t.Error("a unit on day-rate work should be refused")
	}
	if _, err := s.AddEngagement(NewEngagement{Client: "sevalla", Name: "videos", Basis: "item", Unit: "video|x"}); err == nil {
		t.Error("a unit that would break the table should be refused")
	}
}

func TestDeliveriesAndTimeShareTheSheet(t *testing.T) {
	s := itemStore(t)
	if _, err := s.AddDelivery(NewDelivery{Engagement: "sevalla-articles", What: "Queues | a deep dive", Date: "2026-10-02"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDelivery(NewDelivery{Engagement: "sevalla-articles", Items: "2", What: "Two shorts", Date: "2026-10-09"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTime(NewTime{Engagement: "sevalla-articles", Duration: "1d", What: "Research", Date: "2026-10-08"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.SheetPath("sevalla-articles", "2026-10"))
	for _, want := range []string{
		"| Date | Items | Time | What |",
		"| 2026-10-02 | 1 | - | Queues \\| a deep dive |",
		"| 2026-10-09 | 2 | - | Two shorts |",
		"| 2026-10-08 | - | 1d | Research |",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("no %q in\n%s", want, data)
		}
	}
	entries, problems, err := s.TimeEntries()
	if err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	var count, minutes int
	for _, e := range entries {
		count += int(e.Count)
		minutes += e.Minutes
	}
	if count != 300 || minutes != s.DayMinutes {
		t.Errorf("read back %d hundredths of items and %d minutes", count, minutes)
	}
	if entries[0].What != "Queues | a deep dive" {
		t.Errorf("what: %q", entries[0].What)
	}
}

func TestOnlyItemWorkTakesDeliveries(t *testing.T) {
	s := itemStore(t)
	_, err := s.AddDelivery(NewDelivery{Engagement: "sevalla-consulting", Date: "2026-10-02"})
	if err == nil || !strings.Contains(err.Error(), "not item work") {
		t.Fatalf("got %v", err)
	}
	if _, err := s.AddDelivery(NewDelivery{Engagement: "sevalla-articles", Items: "0"}); err == nil {
		t.Error("zero items should be refused")
	}
}

func TestAnOldSheetGainsAnItemsColumn(t *testing.T) {
	s := itemStore(t)
	path := s.SheetPath("sevalla-articles", "2026-10")
	old := "---\ntype: timesheet\nengagement: '[[sevalla-articles]]'\nclient: '[[sevalla]]'\nmonth: 2026-10\n---\n| Date | Time | What |\n|------|------|------|\n| 2026-10-01 | 3h | Outline |\n\nNotes kept below.\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDelivery(NewDelivery{Engagement: "sevalla-articles", What: "Filed", Date: "2026-10-03"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "| Date | Items | Time | What |\n| ------ | ------- | ------ | ------ |\n| 2026-10-01 | - | 3h | Outline |\n| 2026-10-03 | 1 | - | Filed |\n\nNotes kept below.\n"
	if !strings.HasSuffix(string(data), want) {
		t.Errorf("got\n%s", data)
	}
	entries, problems, _ := s.TimeEntries()
	if len(problems) > 0 || len(entries) != 2 || entries[0].Minutes != 180 || entries[1].Count != 100 {
		t.Errorf("read back %+v %v", entries, problems)
	}
}

func TestItemsAreBilledByTheMonth(t *testing.T) {
	s := itemStore(t)
	s.AddClient(NewClient{Slug: "solo", Name: "Solo"})
	for _, d := range []NewDelivery{
		{Engagement: "sevalla-articles", Date: "2026-10-02"},
		{Engagement: "sevalla-articles", Items: "3", Date: "2026-10-20"},
		{Engagement: "sevalla-articles", Date: "2026-11-02"},
	} {
		if _, err := s.AddDelivery(d); err != nil {
			t.Fatal(err)
		}
	}
	inv, skipped, err := s.AddInvoice(NewInvoice{Client: "sevalla", Month: "2026-10"})
	if err != nil {
		t.Fatal(err, skipped)
	}
	if len(inv.Lines) != 1 {
		t.Fatalf("lines: %+v", inv.Lines)
	}
	l := inv.Lines[0]
	if l.Quantity() != "4 articles" || l.Price != 75000 || l.Amount != 300000 {
		t.Errorf("got %+v (%s)", l, l.Quantity())
	}
	if _, _, err := s.AddInvoice(NewInvoice{Client: "sevalla", Month: "2026-10"}); err == nil {
		t.Error("the month was billed twice")
	}
}

func TestTimeWithoutDeliveriesIsNotBilled(t *testing.T) {
	s := itemStore(t)
	if _, err := s.AddTime(NewTime{Engagement: "sevalla-articles", Duration: "1d", Date: "2026-10-05"}); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := s.AddInvoice(NewInvoice{Client: "sevalla", Month: "2026-10"})
	if err == nil {
		t.Fatal("an invoice with nothing delivered was drafted")
	}
	if len(skipped) != 1 || skipped[0].Reason != "time logged but nothing delivered" {
		t.Errorf("skipped: %+v", skipped)
	}
}

func TestStatsValueItemsAndTheirDays(t *testing.T) {
	s := itemStore(t)
	s.AddDelivery(NewDelivery{Engagement: "sevalla-articles", Items: "2", Date: "2026-10-02"})
	s.AddTime(NewTime{Engagement: "sevalla-articles", Duration: "1.5d", Date: "2026-10-02"})
	p, _ := MonthPeriod("2026-10")
	st, _, err := s.Stats(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Engagements) != 1 {
		t.Fatalf("got %+v", st.Engagements)
	}
	e := st.Engagements[0]
	if e.Items != "2" || e.Unit != "article" || e.Value == nil || *e.Value != 150000 || e.PerDay == nil || *e.PerDay != 100000 {
		t.Errorf("got %+v value %v per day %v", e, e.Value, e.PerDay)
	}
}
