package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/JustSteveKing/mavis/internal/record"
)

// ClientStatuses is relationship temperature, moved by hand. mavis may
// suggest a move; it never makes one.
var ClientStatuses = []string{"prospect", "active", "warm", "cold"}

func ValidClientStatus(s string) bool { return slices.Contains(ClientStatuses, s) }

type Client struct {
	Slug        string `json:"slug"`
	Status      string `json:"status"`
	StatusSince string `json:"status_since,omitempty"`
	Name        string `json:"name"`
	Contact     string `json:"contact,omitempty"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Currency    string `json:"currency"`
	TermsDays   int    `json:"terms_days"`
	Created     string `json:"created,omitempty"`
	Path        string `json:"path"`
}

// NewClient is what `client add` supplies.
type NewClient struct {
	Slug, Name, Status, Contact, Email, Phone string
}

func (s *Store) clientPath(slug string) string {
	return filepath.Join(s.root, ClientsDir, slug+".md")
}

func clientFrom(path string, d *record.Document) Client {
	c := Client{
		Slug:        strings.TrimSuffix(filepath.Base(path), ".md"),
		Status:      d.Get("status"),
		StatusSince: d.Get("status_since"),
		Name:        d.Get("name"),
		Contact:     d.Get("contact"),
		Email:       d.Get("email"),
		Phone:       d.Get("phone"),
		Currency:    d.Get("currency"),
		Created:     d.Get("created"),
		Path:        path,
	}
	if c.Name == "" {
		c.Name = c.Slug
	}
	if c.Currency == "" {
		c.Currency = "GBP"
	}
	c.TermsDays = 30
	if n, err := strconv.Atoi(d.Get("terms_days")); err == nil && n > 0 {
		c.TermsDays = n
	}
	return c
}

// Clients lists every client, sorted by slug. Files in clients/ without
// `type: client` (a README, say) are not clients and are skipped quietly;
// files that fail to parse come back as problems.
func (s *Store) Clients() ([]Client, []Problem, error) {
	dir := filepath.Join(s.root, ClientsDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var clients []Client
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
		if d.Get("type") != "client" {
			continue
		}
		clients = append(clients, clientFrom(path, d))
	}
	return clients, problems, nil
}

// AddClient writes a new client note.
func (s *Store) AddClient(in NewClient) (Client, error) {
	if !ValidSlug(in.Slug) {
		return Client{}, fmt.Errorf("%q is not a usable name for a file; try %q", in.Slug, Slugify(in.Slug))
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if !ValidClientStatus(in.Status) {
		return Client{}, fmt.Errorf("status must be one of %s", strings.Join(ClientStatuses, ", "))
	}
	if in.Name == "" {
		in.Name = in.Slug
	}

	var out Client
	err := s.withLock(func() error {
		path := s.clientPath(in.Slug)
		if existing, err := s.findNote(in.Slug); err != nil {
			return err
		} else if existing != "" {
			if existing == filepath.Join(ClientsDir, in.Slug+".md") {
				return fmt.Errorf("client %s already exists", in.Slug)
			}
			return fmt.Errorf("%s already exists, and a client called %s would make [[%s]] ambiguous; pick another name", existing, in.Slug, in.Slug)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}

		today := s.today()
		d := record.New()
		d.Set("type", "client")
		d.Set("status", in.Status)
		d.SetPlain("status_since", today)
		d.Set("name", in.Name)
		for _, f := range []struct{ key, value string }{
			{"contact", in.Contact}, {"email", in.Email}, {"phone", in.Phone},
		} {
			if f.value != "" {
				d.Set(f.key, f.value)
			}
		}
		d.Set("currency", "GBP")
		d.SetPlain("terms_days", "30")
		d.SetPlain("created", today)

		if err := write(path, d); err != nil {
			return err
		}
		out = clientFrom(path, d)
		return nil
	})
	return out, err
}

// ResolveClient finds a client by exact slug, or by a substring of its slug
// or name that matches exactly one. Guessing between several is refused.
func (s *Store) ResolveClient(query string) (Client, error) {
	clients, _, err := s.Clients()
	if err != nil {
		return Client{}, err
	}
	for _, c := range clients {
		if c.Slug == query {
			return c, nil
		}
	}

	q := strings.ToLower(query)
	var matches []Client
	for _, c := range clients {
		if strings.Contains(c.Slug, q) || strings.Contains(strings.ToLower(c.Name), q) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return Client{}, fmt.Errorf("client %q: %w", query, ErrNotFound)
	case 1:
		return matches[0], nil
	}
	amb := &AmbiguousError{Query: query}
	for _, c := range matches {
		amb.Candidates = append(amb.Candidates, c.Slug)
	}
	return Client{}, amb
}

// SetClientStatus moves a client and stamps status_since. Moving a client to
// the status it already has changes nothing and says so.
func (s *Store) SetClientStatus(query, status string) (Client, bool, error) {
	if !ValidClientStatus(status) {
		return Client{}, false, fmt.Errorf("status must be one of %s", strings.Join(ClientStatuses, ", "))
	}

	var out Client
	var changed bool
	err := s.withLock(func() error {
		c, err := s.ResolveClient(query)
		if err != nil {
			return err
		}
		d, err := read(c.Path)
		if err != nil {
			return err
		}
		if d.Get("status") == status {
			out = c
			return nil
		}
		d.Set("status", status)
		d.SetPlain("status_since", s.today())
		if err := write(c.Path, d); err != nil {
			return err
		}
		out, changed = clientFrom(c.Path, d), true
		return nil
	})
	return out, changed, err
}
