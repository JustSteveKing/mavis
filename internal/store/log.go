package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/record"
)

// LogKinds are the things that happen with a client. A note is a log entry
// too: one record type, one timeline per client.
var LogKinds = []string{"call", "meeting", "email", "note"}

func ValidLogKind(s string) bool { return slices.Contains(LogKinds, s) }

const dateTimeLayout = "2006-01-02T15:04"

type LogEntry struct {
	Slug       string     `json:"slug"`
	Kind       string     `json:"kind"`
	Date       string     `json:"date"`
	Client     string     `json:"client"`
	Engagement string     `json:"engagement,omitempty"`
	With       []string   `json:"with,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	FollowUps  []FollowUp `json:"follow_ups,omitempty"`

	// A payment reminder records the invoice it chased and which reminder
	// it was, so the next one knows where the chase has got to.
	Invoice  string `json:"invoice,omitempty"`
	Reminder int    `json:"reminder,omitempty"`

	Path string `json:"path"`
}

// FollowUp is a checkbox in a log entry's body.
type FollowUp struct {
	Text   string `json:"text"`
	Due    string `json:"due,omitempty"`
	Done   bool   `json:"done"`
	Client string `json:"client"`
	Log    string `json:"log"`
	line   int
}

// A checkbox, with an optional "(due YYYY-MM-DD)" at the end. That suffix
// is the only syntax mavis parses out of a body.
var checkbox = regexp.MustCompile(`^(\s*[-*] \[)([ xX])(\]\s+)(.*?)\s*$`)
var dueSuffix = regexp.MustCompile(`\s*\(due (\d{4}-\d{2}-\d{2})\)$`)

func followUpsIn(body, client, log string) []FollowUp {
	var out []FollowUp
	for i, line := range strings.Split(body, "\n") {
		m := checkbox.FindStringSubmatch(line)
		if m == nil || m[4] == "" {
			continue
		}
		f := FollowUp{Text: m[4], Done: m[2] != " ", Client: client, Log: log, line: i}
		if d := dueSuffix.FindStringSubmatch(f.Text); d != nil {
			f.Due = d[1]
			f.Text = strings.TrimSpace(strings.TrimSuffix(f.Text, d[0]))
		}
		out = append(out, f)
	}
	return out
}

// summaryOf is the body up to the first heading or checkbox, which is what
// a listing wants to show.
func summaryOf(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") || checkbox.MatchString(line) {
			break
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func logFrom(path string, d *record.Document) LogEntry {
	l := LogEntry{
		Slug:       strings.TrimSuffix(filepath.Base(path), ".md"),
		Kind:       d.Get("kind"),
		Date:       d.Get("date"),
		Client:     linkTarget(d.Get("client")),
		Engagement: linkTarget(d.Get("engagement")),
		Invoice:    linkTarget(d.Get("invoice")),
		With:       d.List("with"),
		Summary:    summaryOf(d.Body),
		Path:       path,
	}
	l.FollowUps = followUpsIn(d.Body, l.Client, l.Slug)
	if n, err := strconv.Atoi(d.Get("reminder")); err == nil && n > 0 {
		l.Reminder = n
	}
	return l
}

// Logs lists every log entry, oldest first.
func (s *Store) Logs() ([]LogEntry, []Problem, error) {
	dir := filepath.Join(s.root, s.layout.Log)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var out []LogEntry
	var problems []Problem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		d, err := read(path)
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		if d.Get("type") != "log" {
			continue
		}
		out = append(out, logFrom(path, d))
	}
	slices.SortStableFunc(out, func(a, b LogEntry) int { return strings.Compare(a.Date, b.Date) })
	return out, problems, nil
}

type NewFollowUp struct {
	Text, Due string
}

type NewLog struct {
	Kind, Client, Engagement, Summary, Date string
	With                                    []string
	FollowUps                               []NewFollowUp

	// For a payment reminder: the invoice, which reminder this is, and the
	// message as sent, kept in the entry as a record.
	Invoice  string
	Reminder int
	Message  string
}

// AddLog writes a log entry. Date defaults to now, to the minute.
func (s *Store) AddLog(in NewLog) (LogEntry, error) {
	if !ValidLogKind(in.Kind) {
		return LogEntry{}, fmt.Errorf("kind must be one of %s", strings.Join(LogKinds, ", "))
	}
	when := s.Now().Format(dateTimeLayout)
	if in.Date != "" {
		t, err := parseWhen(in.Date)
		if err != nil {
			return LogEntry{}, err
		}
		when = t
	}
	for i, f := range in.FollowUps {
		if strings.TrimSpace(f.Text) == "" {
			return LogEntry{}, errors.New("a follow-up needs some text")
		}
		if f.Due != "" {
			due, err := s.ParseDue(f.Due)
			if err != nil {
				return LogEntry{}, err
			}
			in.FollowUps[i].Due = due
		}
	}

	var out LogEntry
	err := s.withLock(func() error {
		client, err := s.ResolveClient(in.Client)
		if err != nil {
			return err
		}
		var engagement string
		if in.Engagement != "" {
			e, err := s.ResolveEngagement(in.Engagement)
			if err != nil {
				return err
			}
			if e.Client != client.Slug {
				return fmt.Errorf("engagement %s belongs to %s, not %s", e.Slug, e.Client, client.Slug)
			}
			engagement = e.Slug
		}

		dir := filepath.Join(s.root, s.layout.Log)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path, err := freePath(dir, when[:len(dateLayout)]+"-"+client.Slug+"-"+in.Kind)
		if err != nil {
			return err
		}

		d := record.New()
		d.Set("type", "log")
		d.Set("kind", in.Kind)
		d.SetPlain("date", when)
		d.Set("client", link(client.Slug))
		if engagement != "" {
			d.Set("engagement", link(engagement))
		}
		if len(in.With) > 0 {
			d.SetList("with", in.With)
		}
		if in.Invoice != "" {
			d.Set("invoice", link(in.Invoice))
		}
		if in.Reminder > 0 {
			d.SetPlain("reminder", strconv.Itoa(in.Reminder))
		}
		s.stamp(d)

		var body strings.Builder
		if summary := strings.TrimSpace(in.Summary); summary != "" {
			body.WriteString(summary + "\n")
		}
		if len(in.FollowUps) > 0 {
			if body.Len() > 0 {
				body.WriteString("\n")
			}
			body.WriteString("## Follow-ups\n")
			for _, f := range in.FollowUps {
				body.WriteString("- [ ] " + strings.TrimSpace(f.Text))
				if f.Due != "" {
					body.WriteString(" (due " + f.Due + ")")
				}
				body.WriteString("\n")
			}
		}
		if msg := strings.TrimSpace(in.Message); msg != "" {
			if body.Len() > 0 {
				body.WriteString("\n")
			}
			body.WriteString("## As sent\n")
			for _, line := range strings.Split(msg, "\n") {
				body.WriteString(strings.TrimRight("> "+line, " ") + "\n")
			}
		}
		d.Body = body.String()

		if err := write(path, d); err != nil {
			return err
		}
		out = logFrom(path, d)
		return nil
	})
	return out, err
}

// freePath picks name.md, or name-2.md and so on when two calls with one
// client land on the same day.
func freePath(dir, name string) (string, error) {
	for n := 1; n < 1000; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", name, n)
		}
		path := filepath.Join(dir, candidate+".md")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("too many entries named %s", name)
}

func parseWhen(v string) (string, error) {
	if t, err := time.Parse(dateTimeLayout, v); err == nil {
		return t.Format(dateTimeLayout), nil
	}
	if t, err := time.Parse(dateLayout, v); err == nil {
		return t.Format(dateLayout) + "T00:00", nil
	}
	return "", fmt.Errorf("date %q: use YYYY-MM-DD or YYYY-MM-DDTHH:MM", v)
}

var relativeDays = regexp.MustCompile(`^\+(\d+)([dw])$`)

// ParseDue reads a due date: YYYY-MM-DD, or +3d / +2w from today.
func (s *Store) ParseDue(v string) (string, error) {
	if m := relativeDays.FindStringSubmatch(v); m != nil {
		var n int
		fmt.Sscanf(m[1], "%d", &n)
		if m[2] == "w" {
			n *= 7
		}
		return s.Now().AddDate(0, 0, n).Format(dateLayout), nil
	}
	t, err := time.Parse(dateLayout, v)
	if err != nil {
		return "", fmt.Errorf("due %q: use YYYY-MM-DD, or +3d / +2w", v)
	}
	return t.Format(dateLayout), nil
}

// FollowUps lists open follow-ups, soonest due first, undated last.
func (s *Store) FollowUps() ([]FollowUp, []Problem, error) {
	logs, problems, err := s.Logs()
	if err != nil {
		return nil, nil, err
	}
	var open []FollowUp
	for _, l := range logs {
		for _, f := range l.FollowUps {
			if !f.Done {
				open = append(open, f)
			}
		}
	}
	slices.SortStableFunc(open, func(a, b FollowUp) int {
		switch {
		case a.Due == b.Due:
			return 0
		case a.Due == "":
			return 1
		case b.Due == "":
			return -1
		}
		return strings.Compare(a.Due, b.Due)
	})
	return open, problems, nil
}

// CompleteFollowUp ticks the one open follow-up whose text contains query,
// optionally only among one client's. Only that line of the file changes.
func (s *Store) CompleteFollowUp(clientQuery, query string) (FollowUp, error) {
	var out FollowUp
	err := s.withLock(func() error {
		client := ""
		if clientQuery != "" {
			c, err := s.ResolveClient(clientQuery)
			if err != nil {
				return err
			}
			client = c.Slug
		}
		open, _, err := s.FollowUps()
		if err != nil {
			return err
		}

		q := strings.ToLower(query)
		var matches []FollowUp
		for _, f := range open {
			if (client == "" || f.Client == client) && strings.Contains(strings.ToLower(f.Text), q) {
				matches = append(matches, f)
			}
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("open follow-up %q: %w", query, ErrNotFound)
		case 1:
		default:
			amb := &AmbiguousError{Query: query}
			for _, f := range matches {
				amb.Candidates = append(amb.Candidates, fmt.Sprintf("%q (%s)", f.Text, f.Log))
			}
			return amb
		}

		f := matches[0]
		path := filepath.Join(s.root, s.layout.Log, f.Log+".md")
		d, err := read(path)
		if err != nil {
			return err
		}
		lines := strings.Split(d.Body, "\n")
		m := checkbox.FindStringSubmatch(lines[f.line])
		if m == nil || m[2] != " " {
			return fmt.Errorf("%s changed while reading it; try again", path)
		}
		lines[f.line] = m[1] + "x" + m[3] + m[4]
		d.Body = strings.Join(lines, "\n")
		if err := write(path, d); err != nil {
			return err
		}
		f.Done = true
		out = f
		return nil
	})
	return out, err
}

// Reminders lists the payment reminders logged against an invoice, oldest
// first.
func (s *Store) Reminders(number string) ([]LogEntry, error) {
	logs, _, err := s.Logs()
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, l := range logs {
		if l.Invoice == number && l.Reminder > 0 {
			out = append(out, l)
		}
	}
	return out, nil
}

// CompleteFollowUpExact ticks the open follow-up with exactly this text in
// one log entry. Agents use it: they name what they mean rather than have a
// substring matched on their behalf.
func (s *Store) CompleteFollowUpExact(logSlug, text string) (FollowUp, error) {
	var out FollowUp
	err := s.withLock(func() error {
		path := filepath.Join(s.root, s.layout.Log, logSlug+".md")
		d, err := read(path)
		if err != nil {
			return fmt.Errorf("log entry %q: %w", logSlug, ErrNotFound)
		}
		l := logFrom(path, d)
		for _, f := range l.FollowUps {
			if f.Text != text {
				continue
			}
			if f.Done {
				return fmt.Errorf("%q in %s is already done", text, logSlug)
			}
			lines := strings.Split(d.Body, "\n")
			m := checkbox.FindStringSubmatch(lines[f.line])
			lines[f.line] = m[1] + "x" + m[3] + m[4]
			d.Body = strings.Join(lines, "\n")
			if err := write(path, d); err != nil {
				return err
			}
			f.Done = true
			out = f
			return nil
		}
		return fmt.Errorf("no follow-up %q in %s: %w", text, logSlug, ErrNotFound)
	})
	return out, err
}
