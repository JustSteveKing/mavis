package web

import (
	"cmp"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/pdf"
	"github.com/JustSteveKing/mavis/internal/remind"
	"github.com/JustSteveKing/mavis/internal/store"
)

func (srv *server) funcs() template.FuncMap {
	return template.FuncMap{
		"money": amount,
		"moneyp": func(p *money.Pence) string {
			if p == nil {
				return "-"
			}
			return p.Display()
		},
		"hours": duration.Hours,
		"days":  func(m int) string { return duration.Days(m, srv.s.DayMinutes) },
		"spent": func(m int) string {
			if m == 0 {
				return "-"
			}
			return duration.Days(m, srv.s.DayMinutes) + " (" + duration.Hours(m) + ")"
		},
		"rel":   srv.rel,
		"month": monthName,
		"when":  func(d string) string { return strings.Replace(d, "T", " ", 1) },
		"day": func(d string) string {
			if len(d) >= 10 {
				return d[:10]
			}
			return d
		},
		"md":    markdown,
		"join":  strings.Join,
		"pathq": url.PathEscape,
		"today": srv.date,
		"vat": func(bp int) string {
			v := strconv.FormatFloat(float64(bp)/100, 'f', 2, 64)
			return strings.TrimRight(strings.TrimRight(v, "0"), ".") + "%"
		},
		"pair": func(label string, rows any) map[string]any {
			return map[string]any{"Label": label, "Rows": rows}
		},
		"navItems": func() []navItem { return nav },
		"initials": initials,
	}
}

type navItem struct{ Key, Label, Href string }

var nav = []navItem{
	{"today", "Today", "/"},
	{"clients", "Clients", "/clients/"},
	{"engagements", "Engagements", "/engagements/"},
	{"log", "Log", "/log/"},
	{"follow-ups", "Follow-ups", "/follow-ups/"},
	{"time", "Time", "/time/"},
	{"invoices", "Invoices", "/invoices/"},
	{"quotes", "Quotes", "/quotes/"},
	{"stats", "Stats", "/stats/"},
}

// rel says how far a date is from today, in days.
func (srv *server) rel(date string) string {
	if len(date) < 10 {
		return ""
	}
	d, err := time.Parse("2006-01-02", date[:10])
	if err != nil {
		return ""
	}
	t, _ := time.Parse("2006-01-02", srv.date())
	n := int(t.Sub(d).Hours() / 24)
	switch {
	case n == 0:
		return "today"
	case n == 1:
		return "yesterday"
	case n == -1:
		return "tomorrow"
	case n > 0:
		return fmt.Sprintf("%d days ago", n)
	}
	return fmt.Sprintf("in %d days", -n)
}

func monthName(month string) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return t.Format("January 2006")
}

func shiftMonth(month string, by int) string {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return ""
	}
	return t.AddDate(0, by, 0).Format("2006-01")
}

var (
	monthPattern = regexp.MustCompile(`^\d{4}-\d{2}$`)
	yearPattern  = regexp.MustCompile(`^\d{4}$`)
)

// records is everything a page might cross-reference, read once per request.
type records struct {
	clients     []store.Client
	engagements []store.Engagement
	logs        []store.LogEntry
	invoices    []store.Invoice
	quotes      []store.Quote
	time        []store.TimeEntry
	problems    []store.Problem
}

func (srv *server) load() (*records, error) {
	r := &records{}
	var err error
	var p []store.Problem
	if r.clients, p, err = srv.s.Clients(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	if r.engagements, p, err = srv.s.Engagements(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	if r.logs, p, err = srv.s.Logs(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	if r.invoices, p, err = srv.s.Invoices(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	if r.quotes, p, err = srv.s.Quotes(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	if r.time, p, err = srv.s.TimeEntries(); err != nil {
		return nil, err
	}
	r.problems = append(r.problems, p...)
	return r, nil
}

func (r *records) ClientName(slug string) string {
	for _, c := range r.clients {
		if c.Slug == slug {
			return c.Name
		}
	}
	return slug
}

func (r *records) EngagementTitle(slug string) string {
	for _, e := range r.engagements {
		if e.Slug == slug {
			return e.Title
		}
	}
	return slug
}

// newestFirst returns the log reversed: Logs is oldest first.
func newestFirst(logs []store.LogEntry) []store.LogEntry {
	out := slices.Clone(logs)
	slices.Reverse(out)
	return out
}

func overdue(inv store.Invoice, today string) bool {
	return inv.Kind == "invoice" && inv.Status == "issued" && inv.Balance > 0 && inv.Due != "" && inv.Due < today
}

// chase says where the reminders for an overdue invoice have got to, as
// `mavis today` does.
func (srv *server) chase(inv store.Invoice) string {
	earlier, err := srv.s.Reminders(inv.Number)
	if err != nil {
		return ""
	}
	var dates []string
	for _, e := range earlier {
		dates = append(dates, e.Date[:10])
	}
	stage, due, from := remind.Next(inv, dates, srv.date())
	sent := "not chased yet"
	if n := len(dates); n == 1 {
		sent = "reminded " + dates[0]
	} else if n > 1 {
		sent = fmt.Sprintf("reminded %d times, last %s", n, dates[n-1])
	}
	if due {
		return fmt.Sprintf("%s; reminder %d due: mavis invoice remind %s", sent, stage, inv.Number)
	}
	return fmt.Sprintf("%s; next from %s", sent, from)
}

// Named rows for the templates.

type named[T any] struct {
	Item T
	Name string // the client's name
	Also string // an engagement title, or whatever else the row wants
}

type chased struct {
	Invoice store.Invoice
	Name    string
	Chase   string
}

type group[T any] struct {
	Label string
	Rows  []T
}

func (srv *server) todayPage(w http.ResponseWriter, r *http.Request) {
	t, problems, err := srv.s.Today(srv.o.Quiet)
	if err != nil {
		srv.fail(w, err)
		return
	}
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	var unpaid []chased
	for _, inv := range t.Unpaid {
		unpaid = append(unpaid, chased{inv, rec.ClientName(inv.Client), srv.chase(inv)})
	}
	srv.render(w, http.StatusOK, "today", page{
		Title:    "Today",
		Section:  "today",
		Problems: problems,
		Data: map[string]any{
			"Tiles":  srv.tiles(t, rec),
			"T":      t,
			"Unpaid": unpaid,
			"Rec":    rec,
			"Empty": len(t.Overdue)+len(t.Unpaid)+len(t.Quotes)+len(t.Retainers)+len(t.ThisWeek)+
				len(t.Engagements)+len(t.Moves)+len(t.KeepInTouch) == 0,
		},
	})
}

// tile is one of the figures across the top of today.
type tile struct {
	Label, Value, Note, Href string
	Alert                    bool
}

func (srv *server) tiles(t store.Today, rec *records) []tile {
	owed := map[string]money.Pence{}
	for _, inv := range t.Unpaid {
		owed[inv.Currency] += inv.Balance
	}
	quoted := map[string]money.Pence{}
	for _, q := range t.Quotes {
		quoted[q.Currency] += q.Total
	}
	month := srv.date()[:7]
	minutes := 0
	for _, e := range rec.time {
		if e.Date[:7] == month {
			minutes += e.Minutes
		}
	}
	timeValue := "-"
	if minutes > 0 {
		timeValue = duration.Days(minutes, srv.s.DayMinutes)
	}
	return []tile{
		{"Overdue", sums(owed), plural(len(t.Unpaid), "invoice", "invoices") + " past due", "/invoices/", len(t.Unpaid) > 0},
		{"Follow-ups", fmt.Sprint(len(t.Overdue) + len(t.ThisWeek)), fmt.Sprintf("%d overdue, %d this week", len(t.Overdue), len(t.ThisWeek)), "/follow-ups/", len(t.Overdue) > 0},
		{"Quotes waiting", sums(quoted), plural(len(t.Quotes), "quote", "quotes") + " out", "/quotes/", false},
		{"Time this month", timeValue, monthName(month), "/time/", false},
	}
}

// sums writes amounts in several currencies as one figure.
func sums(by map[string]money.Pence) string {
	if len(by) == 0 {
		return "-"
	}
	var parts []string
	for _, c := range slices.Sorted(maps.Keys(by)) {
		parts = append(parts, amount(by[c], c))
	}
	return strings.Join(parts, " + ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// initials are the letters on a client's avatar.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		if r := []rune(w)[0]; unicode.IsLetter(r) || unicode.IsDigit(r) {
			out = append(out, unicode.ToUpper(r))
		}
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

type clientRow struct {
	Client      store.Client
	LastContact string
	Open        int // open follow-ups
	Active      int // active engagements
}

var clientStatuses = []string{"active", "prospect", "warm", "cold"}

func (srv *server) clients(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	rows := map[string][]clientRow{}
	for _, c := range rec.clients {
		row := clientRow{Client: c}
		for _, l := range rec.logs {
			if l.Client != c.Slug {
				continue
			}
			row.LastContact = l.Date
			for _, f := range l.FollowUps {
				if !f.Done {
					row.Open++
				}
			}
		}
		for _, e := range rec.engagements {
			if e.Client == c.Slug && e.Status == "active" {
				row.Active++
			}
		}
		rows[c.Status] = append(rows[c.Status], row)
	}
	srv.render(w, http.StatusOK, "clients", page{
		Title:    "Clients",
		Section:  "clients",
		Problems: rec.problems,
		Data:     grouped(rows, clientStatuses),
	})
}

// grouped orders groups by the statuses given, then any others by name.
func grouped[T any](rows map[string][]T, order []string) []group[T] {
	var out []group[T]
	for _, s := range order {
		if len(rows[s]) > 0 {
			out = append(out, group[T]{s, rows[s]})
		}
	}
	var rest []string
	for s := range rows {
		if !slices.Contains(order, s) {
			rest = append(rest, s)
		}
	}
	slices.Sort(rest)
	for _, s := range rest {
		out = append(out, group[T]{s, rows[s]})
	}
	return out
}

func (srv *server) client(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	slug := r.PathValue("slug")
	i := slices.IndexFunc(rec.clients, func(c store.Client) bool { return c.Slug == slug })
	if i < 0 {
		srv.notFound(w, r, "There is no client "+slug+".")
		return
	}
	c := rec.clients[i]
	body, err := bodyOf(c.Path)
	if err != nil {
		rec.problems = append(rec.problems, store.Problem{Path: c.Path, Err: err})
	}

	var engagements []store.Engagement
	for _, e := range rec.engagements {
		if e.Client == slug {
			engagements = append(engagements, e)
		}
	}
	var logs []named[store.LogEntry]
	var open []store.FollowUp
	for _, l := range newestFirst(rec.logs) {
		if l.Client != slug {
			continue
		}
		logs = append(logs, named[store.LogEntry]{Item: l, Also: rec.EngagementTitle(l.Engagement)})
		for _, f := range l.FollowUps {
			if !f.Done {
				open = append(open, f)
			}
		}
	}
	var invoices []store.Invoice
	for _, inv := range rec.invoices {
		if inv.Client == slug {
			invoices = append(invoices, inv)
		}
	}
	var quotes []store.Quote
	for _, q := range rec.quotes {
		if q.Client == slug {
			quotes = append(quotes, q)
		}
	}
	srv.render(w, http.StatusOK, "client", page{
		Title:    c.Name,
		Section:  "clients",
		Problems: rec.problems,
		Data: map[string]any{
			"C":           c,
			"Body":        body,
			"Engagements": engagements,
			"Logs":        logs,
			"Open":        open,
			"Invoices":    invoices,
			"Quotes":      quotes,
		},
	})
}

var engagementStatuses = []string{"active", "proposed", "paused", "done"}

func (srv *server) engagements(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	rows := map[string][]named[store.Engagement]{}
	for _, e := range rec.engagements {
		rows[e.Status] = append(rows[e.Status], named[store.Engagement]{Item: e, Name: rec.ClientName(e.Client)})
	}
	srv.render(w, http.StatusOK, "engagements", page{
		Title:    "Engagements",
		Section:  "engagements",
		Problems: rec.problems,
		Data:     grouped(rows, engagementStatuses),
	})
}

type monthOfTime struct {
	Month   string
	Entries []store.TimeEntry
	Minutes int
}

// byMonth groups time entries by month, newest month first.
func byMonth(entries []store.TimeEntry) []monthOfTime {
	var out []monthOfTime
	for _, e := range entries {
		m := e.Date[:7]
		if len(out) == 0 || out[len(out)-1].Month != m {
			out = append(out, monthOfTime{Month: m})
		}
		last := &out[len(out)-1]
		last.Entries = append(last.Entries, e)
		last.Minutes += e.Minutes
	}
	slices.Reverse(out)
	return out
}

func (srv *server) engagement(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	slug := r.PathValue("slug")
	i := slices.IndexFunc(rec.engagements, func(e store.Engagement) bool { return e.Slug == slug })
	if i < 0 {
		srv.notFound(w, r, "There is no engagement "+slug+".")
		return
	}
	e := rec.engagements[i]
	body, err := bodyOf(e.Path)
	if err != nil {
		rec.problems = append(rec.problems, store.Problem{Path: e.Path, Err: err})
	}
	var entries []store.TimeEntry
	total := 0
	for _, t := range rec.time {
		if t.Engagement == slug {
			entries = append(entries, t)
			total += t.Minutes
		}
	}
	var logs []store.LogEntry
	for _, l := range newestFirst(rec.logs) {
		if l.Engagement == slug {
			logs = append(logs, l)
		}
	}
	var invoices []store.Invoice
	for _, inv := range rec.invoices {
		if slices.Contains(inv.Engagements, slug) {
			invoices = append(invoices, inv)
		}
	}
	srv.render(w, http.StatusOK, "engagement", page{
		Title:    e.Title,
		Section:  "engagements",
		Problems: rec.problems,
		Data: map[string]any{
			"E":        e,
			"Name":     rec.ClientName(e.Client),
			"Body":     body,
			"Months":   byMonth(entries),
			"Total":    total,
			"Logs":     logs,
			"Invoices": invoices,
		},
	})
}

func (srv *server) logs(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	client, kind := r.URL.Query().Get("client"), r.URL.Query().Get("kind")
	var kinds []string
	var months []group[named[store.LogEntry]]
	for _, l := range newestFirst(rec.logs) {
		if !slices.Contains(kinds, l.Kind) {
			kinds = append(kinds, l.Kind)
		}
		if (client != "" && l.Client != client) || (kind != "" && l.Kind != kind) {
			continue
		}
		m := monthName(l.Date[:min(7, len(l.Date))])
		if len(months) == 0 || months[len(months)-1].Label != m {
			months = append(months, group[named[store.LogEntry]]{Label: m})
		}
		last := &months[len(months)-1]
		last.Rows = append(last.Rows, named[store.LogEntry]{Item: l, Name: rec.ClientName(l.Client), Also: rec.EngagementTitle(l.Engagement)})
	}
	slices.Sort(kinds)
	srv.render(w, http.StatusOK, "log", page{
		Title:    "Log",
		Section:  "log",
		Problems: rec.problems,
		Data: map[string]any{
			"Months":  months,
			"Clients": rec.clients,
			"Kinds":   kinds,
			"Client":  client,
			"Kind":    kind,
		},
	})
}

func (srv *server) logEntry(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	slug := r.PathValue("slug")
	i := slices.IndexFunc(rec.logs, func(l store.LogEntry) bool { return l.Slug == slug })
	if i < 0 {
		srv.notFound(w, r, "There is no log entry "+slug+".")
		return
	}
	l := rec.logs[i]
	body, err := bodyOf(l.Path)
	if err != nil {
		rec.problems = append(rec.problems, store.Problem{Path: l.Path, Err: err})
	}
	title := l.Kind + " with " + rec.ClientName(l.Client)
	if l.Kind == "note" {
		title = "Note on " + rec.ClientName(l.Client)
	}
	srv.render(w, http.StatusOK, "entry", page{
		Title:    strings.ToUpper(title[:1]) + title[1:],
		Section:  "log",
		Problems: rec.problems,
		Data: map[string]any{
			"L":          l,
			"Name":       rec.ClientName(l.Client),
			"Engagement": rec.EngagementTitle(l.Engagement),
			"Body":       body,
		},
	})
}

func (srv *server) followUps(w http.ResponseWriter, r *http.Request) {
	open, problems, err := srv.s.FollowUps()
	if err != nil {
		srv.fail(w, err)
		return
	}
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	rows := make([]named[store.FollowUp], 0, len(open))
	for _, f := range open {
		rows = append(rows, named[store.FollowUp]{Item: f, Name: rec.ClientName(f.Client)})
	}
	srv.render(w, http.StatusOK, "follow-ups", page{
		Title:    "Follow-ups",
		Section:  "follow-ups",
		Problems: problems,
		Data:     rows,
	})
}

type timeSum struct {
	Engagement, Title, Name string
	Minutes                 int
}

func (srv *server) timePage(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	month := r.URL.Query().Get("month")
	if !monthPattern.MatchString(month) {
		month = srv.date()[:7]
	}
	var months []string
	var rows []named[store.TimeEntry]
	sums := map[string]*timeSum{}
	total := 0
	for _, t := range rec.time {
		if m := t.Date[:7]; !slices.Contains(months, m) {
			months = append(months, m)
		}
		if t.Date[:7] != month {
			continue
		}
		rows = append(rows, named[store.TimeEntry]{Item: t, Name: rec.ClientName(t.Client), Also: rec.EngagementTitle(t.Engagement)})
		if sums[t.Engagement] == nil {
			sums[t.Engagement] = &timeSum{t.Engagement, rec.EngagementTitle(t.Engagement), rec.ClientName(t.Client), 0}
		}
		sums[t.Engagement].Minutes += t.Minutes
		total += t.Minutes
	}
	slices.Sort(months)
	slices.Reverse(months)
	var bySlug []timeSum
	for _, s := range sums {
		bySlug = append(bySlug, *s)
	}
	slices.SortFunc(bySlug, func(a, b timeSum) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Title, b.Title))
	})
	srv.render(w, http.StatusOK, "time", page{
		Title:    "Time, " + monthName(month),
		Section:  "time",
		Problems: rec.problems,
		Data: map[string]any{
			"Month":  month,
			"Prev":   shiftMonth(month, -1),
			"Next":   shiftMonth(month, 1),
			"Months": months,
			"Rows":   rows,
			"Sums":   bySlug,
			"Total":  total,
		},
	})
}

type invoiceRow struct {
	Invoice store.Invoice
	Name    string
	Overdue bool
}

func (srv *server) invoices(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	today := srv.date()
	var drafts, issued []invoiceRow
	for _, inv := range rec.invoices {
		row := invoiceRow{inv, rec.ClientName(inv.Client), overdue(inv, today)}
		if inv.Status == "draft" {
			drafts = append(drafts, row)
		} else {
			issued = append(issued, row)
		}
	}
	slices.SortStableFunc(issued, func(a, b invoiceRow) int {
		return cmp.Or(cmp.Compare(b.Invoice.Issued, a.Invoice.Issued), cmp.Compare(b.Invoice.Number, a.Invoice.Number))
	})
	srv.render(w, http.StatusOK, "invoices", page{
		Title:    "Invoices",
		Section:  "invoices",
		Problems: rec.problems,
		Data:     map[string]any{"Drafts": drafts, "Issued": issued},
	})
}

func (srv *server) findInvoice(w http.ResponseWriter, r *http.Request) (*records, store.Invoice, bool) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return nil, store.Invoice{}, false
	}
	slug := r.PathValue("slug")
	i := slices.IndexFunc(rec.invoices, func(inv store.Invoice) bool { return inv.Slug == slug })
	if i < 0 {
		srv.notFound(w, r, "There is no invoice "+slug+".")
		return nil, store.Invoice{}, false
	}
	return rec, rec.invoices[i], true
}

func (srv *server) invoice(w http.ResponseWriter, r *http.Request) {
	rec, inv, ok := srv.findInvoice(w, r)
	if !ok {
		return
	}
	var credits []store.Invoice
	var reminders []store.LogEntry
	if inv.Number != "" {
		for _, c := range rec.invoices {
			if c.Kind == "credit" && c.Credits == inv.Number {
				credits = append(credits, c)
			}
		}
		for _, l := range rec.logs {
			if l.Invoice == inv.Number && l.Reminder > 0 {
				reminders = append(reminders, l)
			}
		}
	}
	var engagements []named[string]
	for _, e := range inv.Engagements {
		engagements = append(engagements, named[string]{Item: e, Name: rec.EngagementTitle(e)})
	}
	chase := ""
	if overdue(inv, srv.date()) {
		chase = srv.chase(inv)
		if chase != "" {
			chase = strings.ToUpper(chase[:1]) + chase[1:] + "."
		}
	}
	title := inv.Number
	if title == "" {
		title = inv.Slug
	}
	srv.render(w, http.StatusOK, "invoice", page{
		Title:    title,
		Section:  "invoices",
		Problems: rec.problems,
		Data: map[string]any{
			"I":           inv,
			"Name":        rec.ClientName(inv.Client),
			"Engagements": engagements,
			"Credits":     credits,
			"Reminders":   reminders,
			"Overdue":     overdue(inv, srv.date()),
			"Chase":       chase,
			"PDF":         srv.pdfExists(inv.Number, inv.Slug),
		},
	})
}

func (srv *server) invoicePDF(w http.ResponseWriter, r *http.Request) {
	if _, inv, ok := srv.findInvoice(w, r); ok {
		srv.servePDF(w, r, pdf.Name(inv.Number, inv.Slug))
	}
}

type quoteRow struct {
	Quote store.Quote
	Name  string
}

func (srv *server) quotes(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	var drafts, sent []quoteRow
	for _, q := range rec.quotes {
		row := quoteRow{q, rec.ClientName(q.Client)}
		if q.Status == "draft" {
			drafts = append(drafts, row)
		} else {
			sent = append(sent, row)
		}
	}
	slices.SortStableFunc(sent, func(a, b quoteRow) int {
		return cmp.Or(cmp.Compare(b.Quote.Sent, a.Quote.Sent), cmp.Compare(b.Quote.Number, a.Quote.Number))
	})
	srv.render(w, http.StatusOK, "quotes", page{
		Title:    "Quotes",
		Section:  "quotes",
		Problems: rec.problems,
		Data:     map[string]any{"Drafts": drafts, "Sent": sent, "Today": srv.date()},
	})
}

func (srv *server) findQuote(w http.ResponseWriter, r *http.Request) (*records, store.Quote, bool) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return nil, store.Quote{}, false
	}
	slug := r.PathValue("slug")
	i := slices.IndexFunc(rec.quotes, func(q store.Quote) bool { return q.Slug == slug })
	if i < 0 {
		srv.notFound(w, r, "There is no quote "+slug+".")
		return nil, store.Quote{}, false
	}
	return rec, rec.quotes[i], true
}

func (srv *server) quote(w http.ResponseWriter, r *http.Request) {
	rec, q, ok := srv.findQuote(w, r)
	if !ok {
		return
	}
	title := q.Number
	if title == "" {
		title = q.Slug
	}
	srv.render(w, http.StatusOK, "quote", page{
		Title:    title + ": " + q.Title,
		Section:  "quotes",
		Problems: rec.problems,
		Data: map[string]any{
			"Q":          q,
			"Name":       rec.ClientName(q.Client),
			"Status":     q.Display(srv.date()),
			"Engagement": rec.EngagementTitle(q.Engagement),
			"PDF":        srv.pdfExists(q.Number, q.Slug),
		},
	})
}

func (srv *server) quotePDF(w http.ResponseWriter, r *http.Request) {
	if _, q, ok := srv.findQuote(w, r); ok {
		srv.servePDF(w, r, pdf.Name(q.Number, q.Slug))
	}
}

func (srv *server) stats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var (
		p                  store.Period
		err                error
		label, prev, next  string
		prevName, nextName string
	)
	if year := q.Get("year"); yearPattern.MatchString(year) {
		p, err = store.YearPeriod(year)
		label = year
		var y int
		fmt.Sscan(year, &y)
		prev, next = fmt.Sprintf("year=%d", y-1), fmt.Sprintf("year=%d", y+1)
		prevName, nextName = fmt.Sprint(y-1), fmt.Sprint(y+1)
	} else {
		month := q.Get("month")
		if !monthPattern.MatchString(month) {
			month = srv.date()[:7]
		}
		p, err = store.MonthPeriod(month)
		label = monthName(month)
		prev, next = "month="+shiftMonth(month, -1), "month="+shiftMonth(month, 1)
		prevName, nextName = monthName(shiftMonth(month, -1)), monthName(shiftMonth(month, 1))
	}
	if err != nil {
		srv.notFound(w, r, err.Error())
		return
	}
	st, problems, err := srv.s.Stats(p)
	if err != nil {
		srv.fail(w, err)
		return
	}
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	srv.render(w, http.StatusOK, "stats", page{
		Title:    "Stats, " + label,
		Section:  "stats",
		Problems: problems,
		Data: map[string]any{
			"S":        st,
			"Rec":      rec,
			"Prev":     prev,
			"Next":     next,
			"PrevName": prevName,
			"NextName": nextName,
			"Year":     p.From[:4],
			"Month":    p.From[:7],
		},
	})
}

// follow resolves a [[wikilink]] to the page for whatever it names.
func (srv *server) follow(w http.ResponseWriter, r *http.Request) {
	rec, err := srv.load()
	if err != nil {
		srv.fail(w, err)
		return
	}
	name := r.PathValue("name")
	to := ""
	switch {
	case slices.ContainsFunc(rec.clients, func(c store.Client) bool { return c.Slug == name }):
		to = "/clients/"
	case slices.ContainsFunc(rec.engagements, func(e store.Engagement) bool { return e.Slug == name }):
		to = "/engagements/"
	case slices.ContainsFunc(rec.logs, func(l store.LogEntry) bool { return l.Slug == name }):
		to = "/log/"
	}
	if to == "" {
		for _, inv := range rec.invoices {
			if inv.Slug == name || (inv.Number != "" && inv.Number == name) {
				to, name = "/invoices/", inv.Slug
			}
		}
		for _, q := range rec.quotes {
			if q.Slug == name || (q.Number != "" && q.Number == name) {
				to, name = "/quotes/", q.Slug
			}
		}
	}
	if to == "" {
		srv.notFound(w, r, "Nothing in the records is called "+name+".")
		return
	}
	http.Redirect(w, r, to+url.PathEscape(name), http.StatusSeeOther)
}
