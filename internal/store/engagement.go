package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/record"
)

// EngagementStatuses is where a piece of work is.
var EngagementStatuses = []string{"proposed", "active", "paused", "done"}

// Bases are how an engagement is billed. Optional until time tracking or
// invoicing needs one.
var Bases = []string{"day", "hourly", "fixed", "retainer"}

func ValidEngagementStatus(s string) bool { return slices.Contains(EngagementStatuses, s) }

type Engagement struct {
	Slug    string `json:"slug"`
	Status  string `json:"status"`
	Client  string `json:"client"`
	Title   string `json:"title"`
	Basis   string `json:"basis,omitempty"`
	Rate    string `json:"rate,omitempty"`
	Budget  string `json:"budget,omitempty"`
	Start   string `json:"start,omitempty"`
	End     string `json:"end,omitempty"`
	Project string `json:"project,omitempty"`
	Path    string `json:"path"`
}

// NewEngagement is what `engagement add` supplies. Name is the short part:
// client acme and name reporting make the file acme-reporting.md.
type NewEngagement struct {
	Client, Name, Title, Status, Basis, Rate, Budget, Start, Project string
}

func engagementFrom(path string, d *record.Document) Engagement {
	e := Engagement{
		Slug:    strings.TrimSuffix(filepath.Base(path), ".md"),
		Status:  d.Get("status"),
		Client:  linkTarget(d.Get("client")),
		Title:   d.Get("title"),
		Basis:   d.Get("basis"),
		Rate:    d.Get("rate"),
		Budget:  d.Get("budget"),
		Start:   d.Get("start"),
		End:     d.Get("end"),
		Project: linkTarget(d.Get("project")),
		Path:    path,
	}
	if e.Title == "" {
		e.Title = e.Slug
	}
	return e
}

// Engagements lists every engagement, sorted by slug.
func (s *Store) Engagements() ([]Engagement, []Problem, error) {
	dir := filepath.Join(s.root, EngagementsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var out []Engagement
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
		if d.Get("type") != "engagement" {
			continue
		}
		out = append(out, engagementFrom(path, d))
	}
	return out, problems, nil
}

// AddEngagement writes a new engagement for an existing client.
func (s *Store) AddEngagement(in NewEngagement) (Engagement, error) {
	if !ValidSlug(in.Name) {
		return Engagement{}, fmt.Errorf("%q is not a usable name for a file; try %q", in.Name, Slugify(in.Name))
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if !ValidEngagementStatus(in.Status) {
		return Engagement{}, fmt.Errorf("status must be one of %s", strings.Join(EngagementStatuses, ", "))
	}
	if in.Basis != "" && !slices.Contains(Bases, in.Basis) {
		return Engagement{}, fmt.Errorf("basis must be one of %s", strings.Join(Bases, ", "))
	}
	if in.Rate != "" {
		rate, err := money.Normalise(in.Rate)
		if err != nil {
			return Engagement{}, fmt.Errorf("rate: %w", err)
		}
		in.Rate = rate
	}
	if in.Budget != "" {
		budget, err := money.Normalise(in.Budget)
		if err != nil {
			return Engagement{}, fmt.Errorf("budget: %w", err)
		}
		in.Budget = budget
	}

	var out Engagement
	err := s.withLock(func() error {
		client, err := s.ResolveClient(in.Client)
		if err != nil {
			return err
		}
		slug := client.Slug + "-" + in.Name
		path := filepath.Join(s.root, EngagementsDir, slug+".md")
		if existing, err := s.findNote(slug); err != nil {
			return err
		} else if existing == filepath.Join(EngagementsDir, slug+".md") {
			return fmt.Errorf("engagement %s already exists", slug)
		} else if existing != "" {
			return fmt.Errorf("%s already exists, and an engagement called %s would make [[%s]] ambiguous; pick another name", existing, slug, slug)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		title := in.Title
		if title == "" {
			title = in.Name
		}
		start := in.Start
		if start == "" && in.Status == "active" {
			start = s.today()
		}

		d := record.New()
		d.Set("type", "engagement")
		d.Set("status", in.Status)
		d.Set("client", link(client.Slug))
		d.Set("title", title)
		if in.Basis != "" {
			d.Set("basis", in.Basis)
		}
		if in.Rate != "" {
			d.Set("rate", in.Rate)
		}
		if in.Budget != "" {
			d.Set("budget", in.Budget)
		}
		if start != "" {
			d.SetPlain("start", start)
		}
		if in.Project != "" {
			d.Set("project", link(linkTarget(in.Project)))
		}

		if err := write(path, d); err != nil {
			return err
		}
		out = engagementFrom(path, d)
		return nil
	})
	return out, err
}

// ResolveEngagement finds an engagement by exact slug, or a substring of its
// slug or title matching exactly one.
func (s *Store) ResolveEngagement(query string) (Engagement, error) {
	all, _, err := s.Engagements()
	if err != nil {
		return Engagement{}, err
	}
	return resolve("engagement", query, all,
		func(e Engagement) string { return e.Slug },
		func(e Engagement) string { return e.Title })
}

// SetEngagementStatus moves an engagement. Moving to done stamps `end`;
// moving to active stamps `start` if it has none.
func (s *Store) SetEngagementStatus(query, status string) (Engagement, bool, error) {
	if !ValidEngagementStatus(status) {
		return Engagement{}, false, fmt.Errorf("status must be one of %s", strings.Join(EngagementStatuses, ", "))
	}

	var out Engagement
	var changed bool
	err := s.withLock(func() error {
		e, err := s.ResolveEngagement(query)
		if err != nil {
			return err
		}
		d, err := read(e.Path)
		if err != nil {
			return err
		}
		if d.Get("status") == status {
			out = e
			return nil
		}
		d.Set("status", status)
		switch status {
		case "done":
			d.SetPlain("end", s.today())
		case "active":
			if d.Get("start") == "" {
				d.SetPlain("start", s.today())
			}
		}
		if err := write(e.Path, d); err != nil {
			return err
		}
		out, changed = engagementFrom(e.Path, d), true
		return nil
	})
	return out, changed, err
}
