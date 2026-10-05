package store

import (
	"os"
	"strings"
	"testing"
)

func rateStore(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	s.AddClient(NewClient{Slug: "env", Name: "Envolutions"})
	if _, err := s.AddEngagement(NewEngagement{Client: "env", Name: "kpz", Title: "KPZ", Basis: "day", Rate: "350", Start: "2026-03-05"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestChangeRateKeepsTheOldOne(t *testing.T) {
	s := rateStore(t)
	e, err := s.ChangeRate("env-kpz", "380", "2026-05-11")
	if err != nil {
		t.Fatal(err)
	}
	if e.Rate != "380.00" || len(e.EarlierRates) != 1 || e.EarlierRates[0].String() != "350.00 until 2026-05-10" {
		t.Fatalf("got %+v", e)
	}
	data, _ := os.ReadFile(e.Path)
	if !strings.Contains(string(data), "earlier_rates: [350.00 until 2026-05-10]") {
		t.Errorf("file:\n%s", data)
	}
	for day, want := range map[string]int64{"2026-03-05": 35000, "2026-05-10": 35000, "2026-05-11": 38000, "2026-09-25T10:00": 38000} {
		if r, ok, err := e.RateOn(day); err != nil || !ok || int64(r) != want {
			t.Errorf("RateOn(%s) = %d %v %v", day, r, ok, err)
		}
	}
	if _, err := s.ChangeRate("env-kpz", "400", "2026-05-01"); err == nil {
		t.Error("a change inside the recorded history should be refused")
	}
	if _, err := s.ChangeRate("env-kpz", "380", "2026-09-01"); err == nil {
		t.Error("changing to the same rate should be refused")
	}
}

func TestChangeRateFromTheStartReplacesIt(t *testing.T) {
	s := rateStore(t)
	e, err := s.ChangeRate("env-kpz", "360", "2026-03-05")
	if err != nil {
		t.Fatal(err)
	}
	if e.Rate != "360.00" || len(e.EarlierRates) != 0 {
		t.Errorf("got %+v", e)
	}
}

func TestAMonthSpanningAChangeBillsEachRate(t *testing.T) {
	s := rateStore(t)
	s.ChangeRate("env-kpz", "380", "2026-05-11")
	s.AddTime(NewTime{Engagement: "env-kpz", Duration: "2d", Date: "2026-05-08"})
	s.AddTime(NewTime{Engagement: "env-kpz", Duration: "1d", Date: "2026-05-15"})
	inv, _, err := s.AddInvoice(NewInvoice{Client: "env", Month: "2026-05"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Lines) != 2 || inv.Lines[0].Price != 35000 || inv.Lines[0].Qty != "2" || inv.Lines[1].Price != 38000 || inv.Lines[1].Qty != "1" {
		t.Fatalf("lines: %+v", inv.Lines)
	}
	if inv.Net != 108000 {
		t.Errorf("net %d", inv.Net)
	}
}

func TestStatsValueEachDayAtItsRate(t *testing.T) {
	s := rateStore(t)
	s.ChangeRate("env-kpz", "380", "2026-05-11")
	s.AddTime(NewTime{Engagement: "env-kpz", Duration: "5d", Date: "2026-03-13"})
	s.AddTime(NewTime{Engagement: "env-kpz", Duration: "5d", Date: "2026-05-22"})
	p, _ := YearPeriod("2026")
	st, problems, err := s.Stats(p)
	if err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	if v := st.Engagements[0].Value; v == nil || *v != 365000 {
		t.Errorf("value %v, want 3,650.00", v)
	}
}

func TestRetainerMonthsAtTheirOwnRate(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme", Name: "Acme"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "care", Basis: "retainer", Rate: "800", Start: "2026-01-01"})
	if _, err := s.ChangeRate("acme-care", "900", "2026-04-01"); err != nil {
		t.Fatal(err)
	}
	p, _ := YearPeriod("2026")
	p.To = "2026-06-30"
	st, _, _ := s.Stats(p)
	if v := st.Engagements[0].Value; v == nil || *v != 3*80000+3*90000 {
		t.Errorf("value %v", v)
	}
	due, _ := s.RetainersDue()
	for _, d := range due {
		want := "900.00"
		if d.Month < "2026-04" {
			want = "800.00"
		}
		if d.Rate != want {
			t.Errorf("%s: %s, want %s", d.Month, d.Rate, want)
		}
	}
}

func TestABadEarlierRateIsReported(t *testing.T) {
	s := rateStore(t)
	e, _ := s.ResolveEngagement("env-kpz")
	data, _ := os.ReadFile(e.Path)
	os.WriteFile(e.Path, []byte(strings.Replace(string(data), "start:", "earlier_rates: [350 from March]\nstart:", 1)), 0o644)
	s.AddTime(NewTime{Engagement: "env-kpz", Duration: "1d", Date: "2026-03-13"})
	p, _ := YearPeriod("2026")
	_, problems, err := s.Stats(p)
	if err != nil || len(problems) == 0 || !strings.Contains(problems[0].Error(), "until") {
		t.Errorf("problems %v %v", problems, err)
	}
}
