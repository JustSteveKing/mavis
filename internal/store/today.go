package store

import (
	"fmt"
	"slices"
	"time"
)

// Quiet is how long, in days, before `today` nudges about a client.
type Quiet struct {
	ActiveQuiet     int // active, no active engagement: move to warm?
	WarmKeepInTouch int // warm: get in touch
	WarmToCold      int // warm: move to cold?
}

// Nudge is a suggestion about a client. mavis suggests moves; it never
// makes them.
type Nudge struct {
	Client      string `json:"client"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Suggest     string `json:"suggest,omitempty"` // a status to move to, if any
	QuietDays   int    `json:"quiet_days"`
	LastContact string `json:"last_contact,omitempty"`
	Reason      string `json:"reason"`
}

type Today struct {
	Date        string       `json:"date"`
	Overdue     []FollowUp   `json:"overdue"`
	Unpaid      []Invoice    `json:"overdue_invoices"`
	ThisWeek    []FollowUp   `json:"this_week"`
	Engagements []Engagement `json:"active_engagements"`
	Moves       []Nudge      `json:"moves"`
	KeepInTouch []Nudge      `json:"keep_in_touch"`
}

// Today gathers what needs attention. Cold clients never appear: working
// through them is a deliberate act, not a daily nag.
func (s *Store) Today(q Quiet) (Today, []Problem, error) {
	now := s.Now()
	today := now.Format(dateLayout)
	weekEnd := now.AddDate(0, 0, 6).Format(dateLayout)
	t := Today{Date: today, Overdue: []FollowUp{}, Unpaid: []Invoice{}, ThisWeek: []FollowUp{}, Engagements: []Engagement{}, Moves: []Nudge{}, KeepInTouch: []Nudge{}}

	clients, problems, err := s.Clients()
	if err != nil {
		return t, nil, err
	}
	engagements, p, err := s.Engagements()
	if err != nil {
		return t, nil, err
	}
	problems = append(problems, p...)
	logs, p, err := s.Logs()
	if err != nil {
		return t, nil, err
	}
	problems = append(problems, p...)

	invoices, p, err := s.Invoices()
	if err != nil {
		return t, nil, err
	}
	problems = append(problems, p...)
	for _, inv := range invoices {
		if inv.Status == "issued" && inv.Due != "" && inv.Due < today {
			t.Unpaid = append(t.Unpaid, inv)
		}
	}

	lastContact := map[string]string{}
	for _, l := range logs {
		if len(l.Date) >= len(dateLayout) {
			if d := l.Date[:len(dateLayout)]; d > lastContact[l.Client] {
				lastContact[l.Client] = d
			}
		}
		for _, f := range l.FollowUps {
			switch {
			case f.Done || f.Due == "":
			case f.Due < today:
				t.Overdue = append(t.Overdue, f)
			case f.Due <= weekEnd:
				t.ThisWeek = append(t.ThisWeek, f)
			}
		}
	}
	byDue := func(a, b FollowUp) int {
		if a.Due < b.Due {
			return -1
		}
		if a.Due > b.Due {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(t.Overdue, byDue)
	slices.SortStableFunc(t.ThisWeek, byDue)

	busy := map[string]bool{}
	for _, e := range engagements {
		if e.Status == "active" {
			t.Engagements = append(t.Engagements, e)
			busy[e.Client] = true
		}
	}

	for _, c := range clients {
		// The quiet clock starts at the later of the last contact and the
		// last status move: moving a client is itself a fresh judgement.
		since := lastContact[c.Slug]
		if c.StatusSince > since {
			since = c.StatusSince
		}
		if since == "" {
			since = c.Created
		}
		quiet, ok := daysBetween(since, now)
		if !ok {
			continue
		}
		n := Nudge{Client: c.Slug, Name: c.Name, Status: c.Status, QuietDays: quiet, LastContact: lastContact[c.Slug]}

		switch c.Status {
		case "active":
			if !busy[c.Slug] && quiet >= q.ActiveQuiet {
				n.Suggest = "warm"
				n.Reason = fmt.Sprintf("active, but no active engagement and quiet for %d days", quiet)
				t.Moves = append(t.Moves, n)
			}
		case "warm":
			if quiet >= q.WarmToCold {
				n.Suggest = "cold"
				n.Reason = fmt.Sprintf("warm, quiet for %d days", quiet)
				t.Moves = append(t.Moves, n)
			} else if quiet >= q.WarmKeepInTouch {
				n.Reason = fmt.Sprintf("warm, quiet for %d days", quiet)
				t.KeepInTouch = append(t.KeepInTouch, n)
			}
		}
	}
	quietest := func(a, b Nudge) int { return b.QuietDays - a.QuietDays }
	slices.SortStableFunc(t.Moves, quietest)
	slices.SortStableFunc(t.KeepInTouch, quietest)

	return t, problems, nil
}

func daysBetween(date string, now time.Time) (int, bool) {
	if len(date) < len(dateLayout) {
		return 0, false
	}
	d, err := time.Parse(dateLayout, date[:len(dateLayout)])
	if err != nil {
		return 0, false
	}
	today, _ := time.Parse(dateLayout, now.Format(dateLayout))
	return int(today.Sub(d).Hours() / 24), true
}
