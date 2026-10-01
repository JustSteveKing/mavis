package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// RetainerDue is a retainer month that has finished and is on no invoice.
type RetainerDue struct {
	Engagement string `json:"engagement"`
	Client     string `json:"client"`
	Title      string `json:"title"`
	Month      string `json:"month"`
	Rate       string `json:"rate"`
}

// RetainersDue lists retainer months ready to bill. Retainers are billed in
// arrears, so a month is due once it is over: November from 1 December.
//
// Months run from the engagement's start to its end, or to the last month
// that has finished, so a forgotten month comes back rather than slipping
// by. A retainer with no start date offers only its latest finished month,
// since there is no telling how far back it goes. Proposed and paused
// retainers are never due. A month on any invoice is not due, unless that
// invoice was credited in full.
func (s *Store) RetainersDue() ([]RetainerDue, error) {
	engagements, _, err := s.Engagements()
	if err != nil {
		return nil, err
	}
	invoices, _, err := s.Invoices()
	if err != nil {
		return nil, err
	}
	billed := map[string]bool{}
	for _, inv := range invoices {
		if inv.Period == "" || inv.FullyCredited() {
			continue
		}
		for _, e := range inv.Engagements {
			billed[e+"/"+inv.Period] = true
		}
	}

	now := s.Now()
	lastDone := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)

	var out []RetainerDue
	for _, e := range engagements {
		if e.Basis != "retainer" || e.Rate == "" || e.Status == "proposed" || e.Status == "paused" {
			continue
		}
		from := lastDone
		if t, err := time.Parse(dateLayout, e.Start); err == nil {
			from = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
		to := lastDone
		if t, err := time.Parse(dateLayout, e.End); err == nil {
			if end := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC); end.Before(to) {
				to = end
			}
		}
		for m := from; !m.After(to); m = m.AddDate(0, 1, 0) {
			month := m.Format(monthLayout)
			if billed[e.Slug+"/"+month] {
				continue
			}
			out = append(out, RetainerDue{Engagement: e.Slug, Client: e.Client, Title: e.Title, Month: month, Rate: e.Rate})
		}
	}
	slices.SortStableFunc(out, func(a, b RetainerDue) int {
		if c := strings.Compare(a.Month, b.Month); c != 0 {
			return c
		}
		return strings.Compare(a.Engagement, b.Engagement)
	})
	return out, nil
}

// DraftRetainer drafts the invoice for one retainer month: the retainer and
// nothing else, as draft-<engagement>-<month>.
func (s *Store) DraftRetainer(engagement, month string) (Invoice, error) {
	e, err := s.ResolveEngagement(engagement)
	if err != nil {
		return Invoice{}, err
	}
	if e.Basis != "retainer" {
		return Invoice{}, fmt.Errorf("%s is %s work, not a retainer", e.Slug, e.Basis)
	}
	inv, skipped, err := s.AddInvoice(NewInvoice{Client: e.Client, Month: month, Engagement: e.Slug})
	if err != nil && len(skipped) > 0 {
		return Invoice{}, fmt.Errorf("%s for %s: %s", e.Slug, month, skipped[0].Reason)
	}
	return inv, err
}
