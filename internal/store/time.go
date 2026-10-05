package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/record"
)

// Timesheets live one per engagement per month, a Markdown table of
// entries. One file per entry would bury the folder; one per month across all
// work would lose the engagement's backlinks. A sheet per engagement per
// month is also exactly what gets invoiced.

const monthLayout = "2006-01"

type TimeEntry struct {
	Date       string `json:"date"`
	Time       string `json:"time,omitempty"`  // as written: 1d, 3h, 1h30m
	Minutes    int    `json:"minutes"`         // against the configured working day
	Items      string `json:"items,omitempty"` // for item work: how many delivered
	What       string `json:"what,omitempty"`
	Engagement string `json:"engagement"`
	Client     string `json:"client"`
	Sheet      string `json:"sheet"`

	// Count is Items as a number, in hundredths like money, so half an
	// article is 50. Zero when the row records time only.
	Count money.Pence `json:"-"`
}

type NewTime struct {
	Engagement, Duration, What, Date string
}

// NewDelivery records things delivered on item work: an article filed, a
// video published. Items defaults to 1.
type NewDelivery struct {
	Engagement, Items, What, Date string
}

const sheetHeader = "| Date | Time | What |\n|------|------|------|\n"

// itemHeader is a sheet for item work: what was delivered, and the time it
// took if you log that too.
const itemHeader = "| Date | Items | Time | What |\n|------|-------|------|------|\n"

// none is how an empty cell is written, so a row reads as deliberate.
const none = "-"

func empty(cell string) bool { return cell == "" || cell == none }

var separatorCell = regexp.MustCompile(`^:?-+:?$`)

// cells splits a table row on unescaped pipes, unescaping \| in what is left.
func cells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if line[i] == '|' {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[i])
	}
	return append(out, strings.TrimSpace(cur.String()))
}

// columns finds where each column is from a sheet's header row. A sheet
// written before item work, | Date | Time | What |, reads as it always did;
// one with an Items column can hold deliveries, time, or both on a row.
func columns(header []string) map[string]int {
	at := map[string]int{}
	for i, c := range header {
		name := strings.ToLower(c)
		if name == "qty" {
			name = "items"
		}
		if _, seen := at[name]; !seen {
			at[name] = i
		}
	}
	return at
}

var defaultColumns = map[string]int{"date": 0, "time": 1, "what": 2}

// parseSheet reads the table rows of a timesheet body. The header and the
// separator are recognised and skipped; any other row that does not read as
// a date with a duration or a count is reported with its line number and
// left out.
func parseSheet(d *record.Document, dayMinutes int, engagement, client, sheet string) ([]TimeEntry, []error) {
	body := d.Body
	var entries []TimeEntry
	var errs []error
	at := defaultColumns
	cell := func(c []string, name string) string {
		if i, ok := at[name]; ok && i < len(c) {
			return c[i]
		}
		return ""
	}
	for i, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		c := cells(line)
		if strings.EqualFold(c[0], "date") {
			at = columns(c)
			continue
		}
		if !slices.ContainsFunc(c, func(s string) bool { return !separatorCell.MatchString(s) }) {
			continue
		}
		if len(c) < 2 {
			errs = append(errs, fmt.Errorf("line %d: want | date | time | what |", d.LineOf(i)))
			continue
		}
		date := cell(c, "date")
		if _, err := time.Parse(dateLayout, date); err != nil {
			errs = append(errs, fmt.Errorf("line %d: %q is not a date (YYYY-MM-DD)", d.LineOf(i), date))
			continue
		}
		e := TimeEntry{Date: date, Engagement: engagement, Client: client, Sheet: sheet}
		if t := cell(c, "time"); !empty(t) {
			minutes, err := duration.Parse(t, dayMinutes)
			if err != nil {
				errs = append(errs, fmt.Errorf("line %d: %w", d.LineOf(i), err))
				continue
			}
			e.Time, e.Minutes = t, minutes
		}
		if n := cell(c, "items"); !empty(n) {
			count, err := money.Parse(n)
			if err != nil || count <= 0 {
				errs = append(errs, fmt.Errorf("line %d: %q items is not a number above zero", d.LineOf(i), n))
				continue
			}
			e.Items, e.Count = trimQty(count), count
		}
		if e.Minutes == 0 && e.Count == 0 {
			errs = append(errs, fmt.Errorf("line %d: no time and no items", d.LineOf(i)))
			continue
		}
		if w, ok := at["what"]; ok && w < len(c) {
			e.What = strings.Join(c[w:], " | ")
		}
		entries = append(entries, e)
	}
	return entries, errs
}

// TimeEntries lists every entry in every timesheet, oldest first.
func (s *Store) TimeEntries() ([]TimeEntry, []Problem, error) {
	dir := filepath.Join(s.root, s.layout.Time)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var out []TimeEntry
	var problems []Problem
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") || strings.HasPrefix(f.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		d, err := read(path)
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		if d.Get("type") != "timesheet" {
			continue
		}
		sheet := strings.TrimSuffix(f.Name(), ".md")
		entries, errs := parseSheet(d, s.DayMinutes, linkTarget(d.Get("engagement")), linkTarget(d.Get("client")), sheet)
		for _, e := range errs {
			problems = append(problems, Problem{Path: path, Err: e})
		}
		out = append(out, entries...)
	}
	slices.SortStableFunc(out, func(a, b TimeEntry) int { return strings.Compare(a.Date, b.Date) })
	return out, problems, nil
}

// ParseDay reads a day: YYYY-MM-DD, today or yesterday.
func (s *Store) ParseDay(v string) (string, error) {
	switch strings.ToLower(v) {
	case "", "today":
		return s.today(), nil
	case "yesterday":
		return s.Now().AddDate(0, 0, -1).Format(dateLayout), nil
	}
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		return "", fmt.Errorf("date %q: use YYYY-MM-DD, today or yesterday", v)
	}
	return t.Format(dateLayout), nil
}

// SheetPath is where an engagement's timesheet for a month lives.
func (s *Store) SheetPath(engagement, month string) string {
	return filepath.Join(s.root, s.layout.Time, engagement+"-"+month+".md")
}

// AddTime appends an entry to the engagement's timesheet for the entry's
// month, creating the sheet if this is the month's first entry.
func (s *Store) AddTime(in NewTime) (TimeEntry, error) {
	raw := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(in.Duration), " ", ""))
	minutes, err := duration.Parse(raw, s.DayMinutes)
	if err != nil {
		return TimeEntry{}, err
	}
	what := strings.Join(strings.Fields(in.What), " ")
	out, err := s.addRow(in.Engagement, in.Date, map[string]string{"time": raw, "what": what}, func(e Engagement) error { return nil })
	out.Time, out.Minutes = raw, minutes
	return out, err
}

// AddDelivery appends things delivered to the engagement's sheet for the
// month. Only item work takes deliveries: a day-rate engagement counts time.
func (s *Store) AddDelivery(in NewDelivery) (TimeEntry, error) {
	raw := strings.TrimSpace(in.Items)
	if raw == "" {
		raw = "1"
	}
	count, err := money.Parse(raw)
	if err != nil || count <= 0 {
		return TimeEntry{}, fmt.Errorf("items %q: use a number above zero", raw)
	}
	items := trimQty(count)
	what := strings.Join(strings.Fields(in.What), " ")
	out, err := s.addRow(in.Engagement, in.Date, map[string]string{"items": items, "what": what}, func(e Engagement) error {
		if e.Basis != "item" {
			basis := e.Basis
			if basis == "" {
				basis = "not set"
			}
			return fmt.Errorf("%s is not item work (its basis is %s); log time with mavis time, or set basis: item and a rate per item on it", e.Slug, basis)
		}
		return nil
	})
	out.Items, out.Count = items, count
	return out, err
}

// addRow writes one row into the engagement's sheet for the row's month,
// starting the sheet if needed. check vets the engagement under the lock.
func (s *Store) addRow(engagement, day string, values map[string]string, check func(Engagement) error) (TimeEntry, error) {
	date, err := s.ParseDay(day)
	if err != nil {
		return TimeEntry{}, err
	}
	values["date"] = date

	var out TimeEntry
	err = s.withLock(func() error {
		e, err := s.ResolveEngagement(engagement)
		if err != nil {
			return err
		}
		if err := check(e); err != nil {
			return err
		}
		month := date[:len(monthLayout)]
		sheet := e.Slug + "-" + month
		path := s.SheetPath(e.Slug, month)

		var d *record.Document
		switch existing, err := s.findNote(sheet); {
		case err != nil:
			return err
		case existing == filepath.Join(s.layout.Time, sheet+".md"):
			if d, err = read(path); err != nil {
				return err
			}
		case existing != "":
			return fmt.Errorf("%s already exists, and a timesheet called %s would make [[%s]] ambiguous", existing, sheet, sheet)
		default:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			d = record.New()
			d.Set("type", "timesheet")
			d.Set("engagement", link(e.Slug))
			d.Set("client", link(e.Client))
			d.Set("month", month)
			d.Body = sheetHeader
			if e.Basis == "item" {
				d.Body = itemHeader
			}
		}

		d.Body = appendRow(d.Body, values)
		if err := write(path, d); err != nil {
			return err
		}
		out = TimeEntry{Date: date, What: values["what"], Engagement: e.Slug, Client: e.Client, Sheet: sheet}
		return nil
	})
	return out, err
}

// appendRow adds a row after the last table row, so text written below the
// table by hand stays below it. With no table at all, it starts one. Cells go
// in the order of the sheet's own header; a value the header has no column
// for (items, on a sheet from before item work) adds that column, filling
// the rows already there with a dash.
func appendRow(body string, values map[string]string) string {
	lines := strings.Split(body, "\n")
	header, last := -1, -1
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		if header < 0 && strings.EqualFold(cells(line)[0], "date") {
			header = i
		}
		last = i
	}
	if header < 0 {
		start := sheetHeader
		if values["items"] != "" {
			start = itemHeader
		}
		if last < 0 {
			if body != "" && !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			if body != "" {
				body += "\n"
			}
			return body + start + row(cells(strings.SplitN(start, "\n", 2)[0]), values) + "\n"
		}
		// A table with no header row: write in the original order.
		return strings.Join(slices.Insert(lines, last+1, row([]string{"Date", "Time", "What"}, values)), "\n")
	}

	names := cells(lines[header])
	if values["items"] != "" {
		if _, ok := columns(names)["items"]; !ok {
			lines = addItemsColumn(lines, header, last)
			names = cells(lines[header])
		}
	}
	return strings.Join(slices.Insert(lines, last+1, row(names, values)), "\n")
}

// row writes values in the order of the header's columns.
func row(header []string, values map[string]string) string {
	out := make([]string, len(header))
	for i, h := range header {
		name := strings.ToLower(h)
		if name == "qty" {
			name = "items"
		}
		v := values[name]
		if v == "" && name != "what" {
			v = none
		}
		out[i] = strings.ReplaceAll(v, "|", `\|`)
	}
	return "| " + strings.Join(out, " | ") + " |"
}

// addItemsColumn puts an Items column after Date in a sheet's table, filling
// existing rows with a dash.
func addItemsColumn(lines []string, header, last int) []string {
	for i := header; i <= last; i++ {
		line := lines[i]
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		c := cells(line)
		add := none
		switch {
		case i == header:
			add = "Items"
		case !slices.ContainsFunc(c, func(s string) bool { return !separatorCell.MatchString(s) }):
			add = "-------"
		}
		c = slices.Insert(c, 1, add)
		for j := range c {
			c[j] = strings.ReplaceAll(c[j], "|", `\|`)
		}
		lines[i] = "| " + strings.Join(c, " | ") + " |"
	}
	return lines
}
