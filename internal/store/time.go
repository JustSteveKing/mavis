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
	"github.com/JustSteveKing/mavis/internal/record"
)

// TimeDir holds timesheets: one per engagement per month, a Markdown table of
// entries. One file per entry would bury the folder; one per month across all
// work would lose the engagement's backlinks. A sheet per engagement per
// month is also exactly what gets invoiced.
const TimeDir = "time"

const monthLayout = "2006-01"

type TimeEntry struct {
	Date       string `json:"date"`
	Time       string `json:"time"`    // as written: 1d, 3h, 1h30m
	Minutes    int    `json:"minutes"` // against the configured working day
	What       string `json:"what,omitempty"`
	Engagement string `json:"engagement"`
	Client     string `json:"client"`
	Sheet      string `json:"sheet"`
}

type NewTime struct {
	Engagement, Duration, What, Date string
}

const sheetHeader = "| Date | Time | What |\n|------|------|------|\n"

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

// parseSheet reads the table rows of a timesheet body. The header and the
// separator are recognised and skipped; any other row that does not read as
// a date and a duration is reported with its line number and left out.
func parseSheet(body string, dayMinutes int, engagement, client, sheet string) ([]TimeEntry, []error) {
	var entries []TimeEntry
	var errs []error
	for i, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		c := cells(line)
		if strings.EqualFold(c[0], "date") {
			continue
		}
		if !slices.ContainsFunc(c, func(s string) bool { return !separatorCell.MatchString(s) }) {
			continue
		}
		if len(c) < 2 {
			errs = append(errs, fmt.Errorf("line %d: want | date | time | what |", i+1))
			continue
		}
		if _, err := time.Parse(dateLayout, c[0]); err != nil {
			errs = append(errs, fmt.Errorf("line %d: %q is not a date (YYYY-MM-DD)", i+1, c[0]))
			continue
		}
		minutes, err := duration.Parse(c[1], dayMinutes)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %d: %w", i+1, err))
			continue
		}
		e := TimeEntry{Date: c[0], Time: c[1], Minutes: minutes, Engagement: engagement, Client: client, Sheet: sheet}
		if len(c) > 2 {
			e.What = strings.Join(c[2:], " | ")
		}
		entries = append(entries, e)
	}
	return entries, errs
}

// TimeEntries lists every entry in every timesheet, oldest first.
func (s *Store) TimeEntries() ([]TimeEntry, []Problem, error) {
	dir := filepath.Join(s.root, TimeDir)
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
		entries, errs := parseSheet(d.Body, s.DayMinutes, linkTarget(d.Get("engagement")), linkTarget(d.Get("client")), sheet)
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
	return filepath.Join(s.root, TimeDir, engagement+"-"+month+".md")
}

// AddTime appends an entry to the engagement's timesheet for the entry's
// month, creating the sheet if this is the month's first entry.
func (s *Store) AddTime(in NewTime) (TimeEntry, error) {
	raw := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(in.Duration), " ", ""))
	minutes, err := duration.Parse(raw, s.DayMinutes)
	if err != nil {
		return TimeEntry{}, err
	}
	date, err := s.ParseDay(in.Date)
	if err != nil {
		return TimeEntry{}, err
	}
	what := strings.Join(strings.Fields(in.What), " ")

	var out TimeEntry
	err = s.withLock(func() error {
		e, err := s.ResolveEngagement(in.Engagement)
		if err != nil {
			return err
		}
		month := date[:len(monthLayout)]
		sheet := e.Slug + "-" + month
		path := s.SheetPath(e.Slug, month)

		var d *record.Document
		switch existing, err := s.findNote(sheet); {
		case err != nil:
			return err
		case existing == filepath.Join(TimeDir, sheet+".md"):
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
		}

		row := fmt.Sprintf("| %s | %s | %s |", date, raw, strings.ReplaceAll(what, "|", `\|`))
		d.Body = appendRow(d.Body, row)
		if err := write(path, d); err != nil {
			return err
		}
		out = TimeEntry{Date: date, Time: raw, Minutes: minutes, What: what, Engagement: e.Slug, Client: e.Client, Sheet: sheet}
		return nil
	})
	return out, err
}

// appendRow adds a row after the last table row, so text written below the
// table by hand stays below it. With no table at all, it starts one.
func appendRow(body, row string) string {
	lines := strings.Split(body, "\n")
	last := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			last = i
		}
	}
	if last < 0 {
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if body != "" {
			body += "\n"
		}
		return body + sheetHeader + row + "\n"
	}
	lines = slices.Insert(lines, last+1, row)
	return strings.Join(lines, "\n")
}
