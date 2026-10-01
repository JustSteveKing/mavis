// Package store is the records directory: where each kind of record lives,
// how it is locked, and how it is written.
//
// Every write holds two locks. A file lock keeps separate processes (the CLI
// and a running MCP server) from writing over each other; flock is per
// process, so an in-process mutex is needed as well for goroutines within one.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/JustSteveKing/mavis/internal/duration"
	"github.com/JustSteveKing/mavis/internal/record"
	"github.com/gofrs/flock"
)

const dateLayout = "2006-01-02"

type Store struct {
	root string
	mu   sync.Mutex
	lock *flock.Flock

	// Now is the clock. Tests replace it.
	Now func() time.Time

	// DayMinutes is a working day, for converting between days and hours.
	DayMinutes int

	layout Layout

	// Actor, when set, is stamped as `by` on every record this store
	// creates. The MCP server sets it to "agent", so a note an agent wrote
	// can always be told from one a person did.
	Actor string
}

// stamp marks a new record with who made it, when that is known.
func (s *Store) stamp(d interface{ Set(string, string) }) {
	if s.Actor != "" {
		d.Set("by", s.Actor)
	}
}

// Open returns the store at root, which must already exist.
func Open(root string) (*Store, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("records directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("records directory %s is not a directory", root)
	}
	lockPath, err := lockFile(root)
	if err != nil {
		return nil, err
	}
	return &Store{root: root, lock: flock.New(lockPath), Now: time.Now, DayMinutes: duration.DefaultDay, layout: DefaultLayout()}, nil
}

// lockFile lives in the user cache rather than the records directory, so a
// vault under git never sees it.
func lockFile(root string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "mavis")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".lock"), nil
}

func (s *Store) Root() string { return s.root }

// Init creates the folders the layout names for records. The files folder
// is made when something is first written to it. Running it again is
// harmless.
func (s *Store) Init() error {
	for _, f := range s.layout.fields() {
		if f.name == "files" {
			continue
		}
		if err := os.MkdirAll(filepath.Join(s.root, f.dir), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) withLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock.Lock(); err != nil {
		return fmt.Errorf("locking records: %w", err)
	}
	defer s.lock.Unlock()
	return fn()
}

func (s *Store) today() string { return s.Now().Format(dateLayout) }

// write replaces path atomically: a reader, or Obsidian, sees the old file
// or the new one, never half of one. The temp file is dot-prefixed so
// Obsidian does not index it in the moment it exists.
func write(path string, d *record.Document) error {
	data, err := d.Bytes()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mavis-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func read(path string) (*record.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := record.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug reports whether s can name a record file.
func ValidSlug(s string) bool { return slugPattern.MatchString(s) }

// Slugify turns a name into a suggested slug: "Acme Ltd" becomes "acme-ltd".
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// findNote looks for a file called name.md anywhere under the root, outside
// dot-directories, and returns its path relative to the root.
//
// Obsidian resolves [[name]] by filename across the whole vault, so a second
// file with the same name makes every link to either one ambiguous.
func (s *Store) findNote(name string) (string, error) {
	target := name + ".md"
	var found string
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != s.root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == target {
			found, _ = filepath.Rel(s.root, path)
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

// ErrNotFound is returned when a name matches nothing.
var ErrNotFound = errors.New("not found")

// AmbiguousError is returned when a name matches more than one record.
type AmbiguousError struct {
	Query      string
	Candidates []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%q matches more than one: %s", e.Query, strings.Join(e.Candidates, ", "))
}

// Problem is a file that could not be read. Listing carries on past it: one
// bad hand edit should not hide every other record.
type Problem struct {
	Path string
	Err  error
}

func (p Problem) Error() string { return fmt.Sprintf("%s: %v", p.Path, p.Err) }
