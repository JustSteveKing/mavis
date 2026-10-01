package store

import (
	"fmt"
	"regexp"
	"strings"
)

// resolve finds one item by exact slug, or by a case-insensitive substring of
// its slug or name that matches exactly one item. Guessing between several
// is refused: quietly acting on the wrong record is worse than retyping.
func resolve[T any](kind, query string, items []T, slug, name func(T) string) (T, error) {
	var zero T
	for _, it := range items {
		if slug(it) == query {
			return it, nil
		}
	}

	q := strings.ToLower(query)
	var matches []T
	for _, it := range items {
		if strings.Contains(slug(it), q) || strings.Contains(strings.ToLower(name(it)), q) {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		return zero, fmt.Errorf("%s %q: %w", kind, query, ErrNotFound)
	case 1:
		return matches[0], nil
	}
	amb := &AmbiguousError{Query: query}
	for _, it := range matches {
		amb.Candidates = append(amb.Candidates, slug(it))
	}
	return zero, amb
}

var wikilink = regexp.MustCompile(`^\[\[([^\]|#]+)(?:[#|][^\]]*)?\]\]$`)

// linkTarget reads a frontmatter reference to another note. mavis writes
// "[[acme]]", but a hand edit may say "acme", "[[acme|Acme Ltd]]" or
// "[[clients/acme]]", and all of them mean the note called acme.
func linkTarget(v string) string {
	v = strings.TrimSpace(v)
	if m := wikilink.FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	v = strings.TrimSuffix(v, ".md")
	if i := strings.LastIndex(v, "/"); i >= 0 {
		v = v[i+1:]
	}
	return strings.TrimSpace(v)
}

func link(slug string) string { return "[[" + slug + "]]" }
