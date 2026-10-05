package store

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
)

// Rates change. An engagement's rate is what it is charged now; the rates
// before it are kept as earlier_rates, a flat list Obsidian can show and
// edit, each "350.00 until 2026-05-10": the rate that applied up to and
// including that day. Time is valued at the rate on its own date, so a rise
// in May does not reprice March.

// EarlierRate is a rate that applied until a day, inclusive.
type EarlierRate struct {
	Rate  string `json:"rate"`
	Until string `json:"until"`
}

func (r EarlierRate) String() string { return r.Rate + " until " + r.Until }

func parseEarlierRates(items []string) ([]EarlierRate, error) {
	var out []EarlierRate
	for _, item := range items {
		rate, until, ok := strings.Cut(strings.TrimSpace(item), " until ")
		if !ok {
			return nil, fmt.Errorf("earlier rate %q: write it as \"350.00 until 2026-05-10\"", item)
		}
		r, err := money.Normalise(strings.TrimSpace(rate))
		if err != nil {
			return nil, fmt.Errorf("earlier rate %q: %w", item, err)
		}
		until = strings.TrimSpace(until)
		if _, err := time.Parse(dateLayout, until); err != nil {
			return nil, fmt.Errorf("earlier rate %q: %q is not a date (YYYY-MM-DD)", item, until)
		}
		out = append(out, EarlierRate{Rate: r, Until: until})
	}
	slices.SortStableFunc(out, func(a, b EarlierRate) int { return strings.Compare(a.Until, b.Until) })
	return out, nil
}

// RateOn is the rate that applied on a day, YYYY-MM-DD (a longer date is
// cut to its day). ok is false when the engagement has no rate at all.
func (e Engagement) RateOn(day string) (rate money.Pence, ok bool, err error) {
	if e.Rate == "" {
		return 0, false, nil
	}
	if e.rateErr != nil {
		return 0, false, e.rateErr
	}
	if len(day) > 10 {
		day = day[:10]
	}
	use := e.Rate
	for _, r := range e.EarlierRates {
		if day <= r.Until {
			use = r.Rate
			break
		}
	}
	rate, err = money.Parse(use)
	if err != nil {
		return 0, false, fmt.Errorf("rate: %w", err)
	}
	return rate, true, nil
}

// ChangeRate makes rate the engagement's rate from a day on, keeping the
// rate it replaces as an earlier rate until the day before. A change on or
// before the start replaces the rate outright, since the old one never
// applied. With no rate yet, it simply sets one.
func (s *Store) ChangeRate(query, rate, from string) (Engagement, error) {
	newRate, err := money.Normalise(rate)
	if err != nil {
		return Engagement{}, fmt.Errorf("rate: %w", err)
	}
	day, err := s.ParseDay(from)
	if err != nil {
		return Engagement{}, err
	}
	var out Engagement
	err = s.withLock(func() error {
		e, err := s.ResolveEngagement(query)
		if err != nil {
			return err
		}
		if e.rateErr != nil {
			return fmt.Errorf("%s: %w", e.Slug, e.rateErr)
		}
		d, err := read(e.Path)
		if err != nil {
			return err
		}
		earlier := e.EarlierRates
		switch {
		case e.Rate == "", e.Start != "" && day <= e.Start:
		case e.Rate == newRate:
			return fmt.Errorf("%s is already %s", e.Slug, newRate)
		default:
			if n := len(earlier); n > 0 && day <= earlier[n-1].Until {
				return fmt.Errorf("%s already had %s until %s; a change has to come after that", e.Slug, earlier[n-1].Rate, earlier[n-1].Until)
			}
			t, _ := time.Parse(dateLayout, day)
			earlier = append(earlier, EarlierRate{Rate: e.Rate, Until: t.AddDate(0, 0, -1).Format(dateLayout)})
		}
		d.Set("rate", newRate)
		if len(earlier) > 0 {
			items := make([]string, len(earlier))
			for i, r := range earlier {
				items[i] = r.String()
			}
			d.SetList("earlier_rates", items)
		}
		if err := write(e.Path, d); err != nil {
			return err
		}
		out = engagementFrom(e.Path, d)
		return nil
	})
	return out, err
}

// rateGroups splits an amount of work by the rate on each entry's date, in
// the order the rates first apply, so a month that spans a change bills
// each part at its own rate.
type rateGroup struct {
	rate    money.Pence
	minutes int
	count   money.Pence
}

func groupByRate(e Engagement, entries []TimeEntry) ([]rateGroup, error) {
	var out []rateGroup
	for _, t := range entries {
		rate, ok, err := e.RateOn(t.Date)
		if err != nil || !ok {
			return nil, err
		}
		i := slices.IndexFunc(out, func(g rateGroup) bool { return g.rate == rate })
		if i < 0 {
			out = append(out, rateGroup{rate: rate})
			i = len(out) - 1
		}
		out[i].minutes += t.Minutes
		out[i].count += t.Count
	}
	return out, nil
}
