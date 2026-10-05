package store

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
)

// Period is an inclusive range of days, YYYY-MM-DD.
type Period struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (p Period) contains(day string) bool { return day >= p.From && day <= p.To }

// MonthPeriod is one calendar month, from YYYY-MM.
func MonthPeriod(month string) (Period, error) {
	t, err := time.Parse(monthLayout, month)
	if err != nil {
		return Period{}, fmt.Errorf("month %q: use YYYY-MM", month)
	}
	return Period{From: t.Format(dateLayout), To: t.AddDate(0, 1, -1).Format(dateLayout)}, nil
}

// YearPeriod is one calendar year, from YYYY.
func YearPeriod(year string) (Period, error) {
	t, err := time.Parse("2006", year)
	if err != nil {
		return Period{}, fmt.Errorf("year %q: use YYYY", year)
	}
	return Period{From: t.Format(dateLayout), To: t.AddDate(1, 0, -1).Format(dateLayout)}, nil
}

// EngagementStat is one engagement's time and value within a period.
//
// Value is time at the engagement's rate: never revenue, which only invoices
// can answer. It is nil where it cannot be worked out: no rate, no basis, or
// fixed-price work, whose fee does not belong to any one period.
type EngagementStat struct {
	Engagement string       `json:"engagement"`
	Client     string       `json:"client"`
	Title      string       `json:"title"`
	Basis      string       `json:"basis,omitempty"`
	Rate       string       `json:"rate,omitempty"`
	Currency   string       `json:"currency"`
	Minutes    int          `json:"minutes"`
	Items      string       `json:"items,omitempty"` // for item work, delivered in the period
	Unit       string       `json:"unit,omitempty"`
	Value      *money.Pence `json:"value_pence,omitempty"`
	PerDay     *money.Pence `json:"per_day_pence,omitempty"`
}

// FixedStat is fixed-price work measured to date, not per period: what the
// budget works out to per day logged so far.
type FixedStat struct {
	Engagement string       `json:"engagement"`
	Client     string       `json:"client"`
	Title      string       `json:"title"`
	Status     string       `json:"status"`
	Currency   string       `json:"currency"`
	Budget     money.Pence  `json:"budget_pence"`
	Minutes    int          `json:"minutes"`
	PerDay     *money.Pence `json:"per_day_pence,omitempty"`
}

// Total is per currency. Amounts in different currencies are never added.
type Total struct {
	Currency string       `json:"currency"`
	Minutes  int          `json:"minutes"`
	Value    money.Pence  `json:"value_pence"`
	PerDay   *money.Pence `json:"per_day_pence,omitempty"`
}

// InvoiceTotal sums invoices in one currency.
type InvoiceTotal struct {
	Currency string      `json:"currency"`
	Count    int         `json:"count"`
	Net      money.Pence `json:"net_pence"`
	VAT      money.Pence `json:"vat_pence"`
	Total    money.Pence `json:"total_pence"`
}

// Invoicing is what invoices say, as opposed to what time is worth. It is
// absent, not zero, until something has been issued.
type Invoicing struct {
	Invoiced    []InvoiceTotal `json:"invoiced"`    // issued in the period
	Credited    []InvoiceTotal `json:"credited"`    // credit notes issued in the period
	Paid        []InvoiceTotal `json:"paid"`        // paid in the period
	Outstanding []InvoiceTotal `json:"outstanding"` // unpaid now, whenever issued
	Overdue     []InvoiceTotal `json:"overdue"`     // unpaid now and past due
}

// QuoteTotal sums quotes' net value in one currency: the work offered,
// before VAT.
type QuoteTotal struct {
	Currency string      `json:"currency"`
	Count    int         `json:"count"`
	Net      money.Pence `json:"net_pence"`
}

// Quoting is the pipeline. Absent until a quote has been sent.
type Quoting struct {
	Sent     []QuoteTotal `json:"sent"`     // sent in the period
	Accepted []QuoteTotal `json:"accepted"` // accepted in the period
	Declined []QuoteTotal `json:"declined"` // declined in the period
	Waiting  []QuoteTotal `json:"waiting"`  // sent, unanswered and still valid now
	Expired  []QuoteTotal `json:"expired"`  // sent, unanswered and past valid_until now
}

type Stats struct {
	Period      Period           `json:"period"`
	Minutes     int              `json:"minutes"`
	Engagements []EngagementStat `json:"engagements"`
	Totals      []Total          `json:"totals"`
	Fixed       []FixedStat      `json:"fixed"`
	Invoicing   *Invoicing       `json:"invoicing,omitempty"`
	Quoting     *Quoting         `json:"quoting,omitempty"`
}

// Stats works out time and its value for a period.
//
//   - day: days logged times the rate
//   - hourly: hours logged times the rate
//   - retainer: the monthly rate for each month of the period the engagement
//     was running, whether or not time was logged
//   - fixed: no value per period; reported to date in Fixed instead
//
// A total's per-day figure answers what logged time earned: value over days,
// counting only engagements with both a value and time in the period. So
// unpriced and fixed-price time does not drag it down, and a retainer with
// nothing logged does not inflate it.
func (s *Store) Stats(p Period) (Stats, []Problem, error) {
	out := Stats{Period: p, Engagements: []EngagementStat{}, Totals: []Total{}, Fixed: []FixedStat{}}

	clients, problems, err := s.Clients()
	if err != nil {
		return out, nil, err
	}
	engagements, pr, err := s.Engagements()
	if err != nil {
		return out, nil, err
	}
	problems = append(problems, pr...)
	entries, pr, err := s.TimeEntries()
	if err != nil {
		return out, nil, err
	}
	problems = append(problems, pr...)

	currency := map[string]string{}
	for _, c := range clients {
		currency[c.Slug] = c.Currency
	}
	inPeriod := map[string]int{}
	toDate := map[string]int{}
	items := map[string]money.Pence{}
	worked := map[string][]TimeEntry{}
	for _, e := range entries {
		toDate[e.Engagement] += e.Minutes
		if p.contains(e.Date) {
			inPeriod[e.Engagement] += e.Minutes
			items[e.Engagement] += e.Count
			worked[e.Engagement] = append(worked[e.Engagement], e)
			out.Minutes += e.Minutes
		}
	}

	day := int64(s.DayMinutes)
	totals := map[string]*Total{}
	valuedMinutes := map[string]int{}
	earned := map[string]money.Pence{}

	for _, e := range engagements {
		cur := currency[e.Client]
		if cur == "" {
			cur = "GBP"
		}

		if e.Basis == "fixed" {
			if e.Budget != "" && (toDate[e.Slug] > 0 || e.Status == "active") {
				budget, err := money.Parse(e.Budget)
				if err != nil {
					problems = append(problems, Problem{Path: e.Path, Err: fmt.Errorf("budget: %w", err)})
					continue
				}
				f := FixedStat{Engagement: e.Slug, Client: e.Client, Title: e.Title, Status: e.Status, Currency: cur, Budget: budget, Minutes: toDate[e.Slug]}
				if f.Minutes > 0 {
					perDay := budget.MulDiv(day, int64(f.Minutes))
					f.PerDay = &perDay
				}
				out.Fixed = append(out.Fixed, f)
			}
			if inPeriod[e.Slug] == 0 {
				continue
			}
		}

		months := 0
		if e.Basis == "retainer" {
			months = retainerMonths(e, p)
		}
		if inPeriod[e.Slug] == 0 && months == 0 && items[e.Slug] == 0 {
			continue
		}

		st := EngagementStat{Engagement: e.Slug, Client: e.Client, Title: e.Title, Basis: e.Basis, Rate: e.Rate, Currency: cur, Minutes: inPeriod[e.Slug]}
		if e.Basis == "item" {
			st.Unit = e.UnitName()
			if n := items[e.Slug]; n > 0 {
				st.Items = trimQty(n)
			}
		}
		if v, ok, err := s.value(e, worked[e.Slug], p); err != nil {
			problems = append(problems, Problem{Path: e.Path, Err: err})
		} else if ok {
			st.Value = &v
			if st.Minutes > 0 {
				perDay := v.MulDiv(day, int64(st.Minutes))
				st.PerDay = &perDay
			}
		}
		out.Engagements = append(out.Engagements, st)

		t := totals[cur]
		if t == nil {
			t = &Total{Currency: cur}
			totals[cur] = t
		}
		t.Minutes += st.Minutes
		if st.Value != nil {
			t.Value += *st.Value
			if st.Minutes > 0 {
				valuedMinutes[cur] += st.Minutes
				earned[cur] += *st.Value
			}
		}
	}

	slices.SortStableFunc(out.Engagements, func(a, b EngagementStat) int {
		if c := strings.Compare(a.Client, b.Client); c != 0 {
			return c
		}
		return strings.Compare(a.Engagement, b.Engagement)
	})
	inv, pr, err := s.invoicing(p)
	if err != nil {
		return out, nil, err
	}
	problems = append(problems, pr...)
	out.Invoicing = inv
	quoting, pr, err := s.quoting(p)
	if err != nil {
		return out, nil, err
	}
	problems = append(problems, pr...)
	out.Quoting = quoting

	for _, cur := range sortedKeys(totals) {
		t := totals[cur]
		if m := valuedMinutes[cur]; m > 0 {
			perDay := earned[cur].MulDiv(day, int64(m))
			t.PerDay = &perDay
		}
		out.Totals = append(out.Totals, *t)
	}
	return out, problems, nil
}

// value is what an engagement's work in a period is worth, each part at the
// rate on its own date. ok is false for work that has no value by time:
// fixed price, or no basis or rate.
func (s *Store) value(e Engagement, worked []TimeEntry, p Period) (money.Pence, bool, error) {
	if e.Rate == "" {
		return 0, false, nil
	}
	if _, err := money.Parse(e.Rate); err != nil {
		return 0, false, fmt.Errorf("rate: %w", err)
	}
	var v money.Pence
	switch e.Basis {
	case "day", "hourly", "item":
		groups, err := groupByRate(e, worked)
		if err != nil {
			return 0, false, err
		}
		for _, g := range groups {
			switch e.Basis {
			case "day":
				v += g.rate.MulDiv(int64(g.minutes), int64(s.DayMinutes))
			case "hourly":
				v += g.rate.MulDiv(int64(g.minutes), 60)
			case "item":
				v += g.rate.MulDiv(int64(g.count), 100)
			}
		}
	case "retainer":
		for _, month := range retainerMonthList(e, p) {
			rate, _, err := e.RateOn(month + "-01")
			if err != nil {
				return 0, false, err
			}
			v += rate
		}
	default:
		return 0, false, nil
	}
	return v, true, nil
}

// retainerMonths counts the calendar months in p during which a retainer
// was running: from its start (or, without one, the first month in p) to its
// end, or open-ended if it has none. Proposed work has not started.
func retainerMonths(e Engagement, p Period) int { return len(retainerMonthList(e, p)) }

// retainerMonthList is those months, YYYY-MM.
func retainerMonthList(e Engagement, p Period) []string {
	if e.Status == "proposed" {
		return nil
	}
	from, to := p.From[:7], p.To[:7]
	if len(e.Start) >= 7 && e.Start[:7] > from {
		from = e.Start[:7]
	}
	if len(e.End) >= 7 && e.End[:7] < to {
		to = e.End[:7]
	}
	if from > to {
		return nil
	}
	f, err1 := time.Parse(monthLayout, from)
	t, err2 := time.Parse(monthLayout, to)
	if err1 != nil || err2 != nil {
		return nil
	}
	var out []string
	for m := f; !m.After(t); m = m.AddDate(0, 1, 0) {
		out = append(out, m.Format(monthLayout))
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (s *Store) invoicing(p Period) (*Invoicing, []Problem, error) {
	invoices, problems, err := s.Invoices()
	if err != nil {
		return nil, nil, err
	}
	today := s.today()
	invoiced, credited, paid, outstanding, overdue := map[string]*InvoiceTotal{}, map[string]*InvoiceTotal{}, map[string]*InvoiceTotal{}, map[string]*InvoiceTotal{}, map[string]*InvoiceTotal{}
	add := func(m map[string]*InvoiceTotal, inv Invoice) {
		t := m[inv.Currency]
		if t == nil {
			t = &InvoiceTotal{Currency: inv.Currency}
			m[inv.Currency] = t
		}
		t.Count++
		t.Net += inv.Net
		t.VAT += inv.VAT
		t.Total += inv.Total
	}
	any := false
	for _, inv := range invoices {
		if inv.Status == "draft" {
			continue
		}
		any = true
		if inv.Kind == "credit" {
			if p.contains(inv.Issued) {
				add(credited, inv)
			}
			continue
		}
		if p.contains(inv.Issued) {
			add(invoiced, inv)
		}
		if inv.Status == "paid" && p.contains(inv.Paid) {
			add(paid, inv)
		}
		if inv.Balance > 0 {
			owed := inv
			owed.Total, owed.Net, owed.VAT = inv.Balance, 0, 0
			add(outstanding, owed)
			if inv.Due < today {
				add(overdue, owed)
			}
		}
	}
	if !any {
		return nil, problems, nil
	}
	list := func(m map[string]*InvoiceTotal) []InvoiceTotal {
		out := []InvoiceTotal{}
		for _, k := range sortedKeys(m) {
			out = append(out, *m[k])
		}
		return out
	}
	return &Invoicing{Invoiced: list(invoiced), Credited: list(credited), Paid: list(paid), Outstanding: list(outstanding), Overdue: list(overdue)}, problems, nil
}

func (s *Store) quoting(p Period) (*Quoting, []Problem, error) {
	quotes, problems, err := s.Quotes()
	if err != nil {
		return nil, nil, err
	}
	today := s.today()
	buckets := map[string]map[string]*QuoteTotal{}
	add := func(bucket string, q Quote) {
		m := buckets[bucket]
		if m == nil {
			m = map[string]*QuoteTotal{}
			buckets[bucket] = m
		}
		t := m[q.Currency]
		if t == nil {
			t = &QuoteTotal{Currency: q.Currency}
			m[q.Currency] = t
		}
		t.Count++
		t.Net += q.Net
	}
	any := false
	for _, q := range quotes {
		if q.Status == "draft" {
			continue
		}
		any = true
		if p.contains(q.Sent) {
			add("sent", q)
		}
		if p.contains(q.Decided) {
			add(q.Status, q)
		}
		if q.Status == "sent" {
			if q.Expired(today) {
				add("expired", q)
			} else {
				add("waiting", q)
			}
		}
	}
	if !any {
		return nil, problems, nil
	}
	list := func(bucket string) []QuoteTotal {
		out := []QuoteTotal{}
		for _, k := range sortedKeys(buckets[bucket]) {
			out = append(out, *buckets[bucket][k])
		}
		return out
	}
	return &Quoting{Sent: list("sent"), Accepted: list("accepted"), Declined: list("declined"), Waiting: list("waiting"), Expired: list("expired")}, problems, nil
}
