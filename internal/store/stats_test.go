package store

import (
	"os"
	"strings"
	"testing"

	"github.com/JustSteveKing/mavis/internal/money"
)

func pence(p *money.Pence) string {
	if p == nil {
		return "nil"
	}
	return p.String()
}

func TestStatsValuesEachBasis(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddClient(NewClient{Slug: "globex"})
	s.AddClient(NewClient{Slug: "initech"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "reporting", Basis: "day", Rate: "650"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "fixes", Basis: "hourly", Rate: "90"})
	s.AddEngagement(NewEngagement{Client: "globex", Name: "support", Basis: "retainer", Rate: "1500", Start: "2026-09-01"})
	s.AddEngagement(NewEngagement{Client: "initech", Name: "rebuild", Basis: "fixed", Budget: "12000"})
	s.AddEngagement(NewEngagement{Client: "initech", Name: "advice"}) // no basis, no rate

	log := func(e, d, date string) {
		if _, err := s.AddTime(NewTime{Engagement: e, Duration: d, Date: date}); err != nil {
			t.Fatal(err)
		}
	}
	log("reporting", "1d", "2026-10-06")
	log("reporting", "1h30m", "2026-10-07") // 1.2d in all
	log("fixes", "2h", "2026-10-08")
	log("rebuild", "10d", "2026-09-15") // before the period, counts to date
	log("rebuild", "2d", "2026-10-09")
	log("advice", "1h", "2026-10-10")
	log("reporting", "1d", "2026-09-30") // outside October

	p, _ := MonthPeriod("2026-10")
	st, problems, err := s.Stats(p)
	if err != nil || len(problems) != 0 {
		t.Fatal(err, problems)
	}

	got := map[string]string{}
	for _, e := range st.Engagements {
		got[e.Engagement] = pence(e.Value) + " / " + pence(e.PerDay)
	}
	want := map[string]string{
		"acme-reporting":  "780.00 / 650.00", // 1.2d at 650
		"acme-fixes":      "180.00 / 675.00", // 2h at 90, 7.5h day
		"globex-support":  "1500.00 / nil",   // one month, no time logged
		"initech-rebuild": "nil / nil",       // fixed: no per-period value
		"initech-advice":  "nil / nil",       // nothing to value it with
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
	if len(st.Engagements) != len(want) {
		t.Errorf("engagements = %v", got)
	}

	if len(st.Totals) != 1 || st.Totals[0].Value != 246000 {
		t.Fatalf("totals = %+v", st.Totals)
	}
	// Per day counts only valued rows with time: 960.00 over 11h (1.4667d)
	// is 654.55. The retainer's 1500.00 with nothing logged must not inflate
	// it, and the unpriced hour must not dilute it.
	if pence(st.Totals[0].PerDay) != "654.55" {
		t.Errorf("per day = %s", pence(st.Totals[0].PerDay))
	}

	if len(st.Fixed) != 1 || st.Fixed[0].Minutes != 12*450 || pence(st.Fixed[0].PerDay) != "1000.00" {
		t.Errorf("fixed = %+v", st.Fixed)
	}
}

func TestCurrenciesAreNeverAddedTogether(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	c, _ := s.AddClient(NewClient{Slug: "euro"})
	data, _ := os.ReadFile(c.Path)
	os.WriteFile(c.Path, []byte(strings.Replace(string(data), "currency: GBP", "currency: EUR", 1)), 0o644)

	s.AddEngagement(NewEngagement{Client: "acme", Name: "a", Basis: "day", Rate: "500"})
	s.AddEngagement(NewEngagement{Client: "euro", Name: "b", Basis: "day", Rate: "600"})
	s.AddTime(NewTime{Engagement: "acme-a", Duration: "1d"})
	s.AddTime(NewTime{Engagement: "euro-b", Duration: "1d"})

	p, _ := MonthPeriod("2026-10")
	st, _, _ := s.Stats(p)
	if len(st.Totals) != 2 || st.Totals[0].Currency != "EUR" || st.Totals[0].Value != 60000 || st.Totals[1].Value != 50000 {
		t.Fatalf("totals = %+v", st.Totals)
	}
}

func TestRetainerMonths(t *testing.T) {
	year, _ := YearPeriod("2026")
	for name, c := range map[string]struct {
		e    Engagement
		want int
	}{
		"whole year, open":       {Engagement{Start: "2025-06-01"}, 12},
		"started in March":       {Engagement{Start: "2026-03-15"}, 10},
		"ended in May":           {Engagement{Start: "2026-01-01", End: "2026-05-31"}, 5},
		"ended before the year":  {Engagement{Start: "2025-01-01", End: "2025-12-31"}, 0},
		"proposed":               {Engagement{Status: "proposed"}, 0},
		"no start: whole period": {Engagement{}, 12},
	} {
		if got := retainerMonths(c.e, year); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}
