package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout is where each kind of record lives, relative to the root. The
// defaults are the folder names mavis has always used; a layout can rename
// or nest them (business/clients) so mavis can sit inside an existing vault
// on its conventions rather than imposing its own.
type Layout struct {
	Clients     string `yaml:"clients" json:"clients"`
	Engagements string `yaml:"engagements" json:"engagements"`
	Log         string `yaml:"log" json:"log"`
	Time        string `yaml:"time" json:"time"`
	Invoices    string `yaml:"invoices" json:"invoices"`
	Quotes      string `yaml:"quotes" json:"quotes"`
	// Files holds generated PDFs and e-invoice XML. Dot-prefixed by default
	// so Obsidian leaves the binaries out of its index.
	Files string `yaml:"files" json:"files"`
}

// DefaultLayout is the layout mavis uses unless told otherwise.
func DefaultLayout() Layout {
	return Layout{
		Clients: "clients", Engagements: "engagements", Log: "log", Time: "time",
		Invoices: "invoices", Quotes: "quotes", Files: ".invoices",
	}
}

// fields pairs each folder with what it holds, in a fixed order.
func (l Layout) fields() []struct{ name, dir string } {
	return []struct{ name, dir string }{
		{"clients", l.Clients}, {"engagements", l.Engagements}, {"log", l.Log}, {"time", l.Time},
		{"invoices", l.Invoices}, {"quotes", l.Quotes}, {"files", l.Files},
	}
}

// withDefaults fills anything left blank with the default.
func (l Layout) withDefaults() Layout {
	d := DefaultLayout()
	for _, f := range []struct {
		got  *string
		want string
	}{
		{&l.Clients, d.Clients}, {&l.Engagements, d.Engagements}, {&l.Log, d.Log}, {&l.Time, d.Time},
		{&l.Invoices, d.Invoices}, {&l.Quotes, d.Quotes}, {&l.Files, d.Files},
	} {
		if strings.TrimSpace(*f.got) == "" {
			*f.got = f.want
		}
	}
	return l
}

// Validate refuses a layout that would put records outside the root, or
// two kinds of record in one folder, or one inside another: mavis tells
// records apart by folder as well as by type.
func (l Layout) Validate() error {
	seen := map[string]string{}
	for _, f := range l.fields() {
		clean := filepath.Clean(f.dir)
		if filepath.IsAbs(f.dir) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("layout.%s is %q: folders are relative to the records folder and stay inside it", f.name, f.dir)
		}
		if clean == "." {
			return fmt.Errorf("layout.%s is the records folder itself: give it a folder of its own", f.name)
		}
		for other, dir := range seen {
			if clean == dir || strings.HasPrefix(clean, dir+string(filepath.Separator)) || strings.HasPrefix(dir, clean+string(filepath.Separator)) {
				return fmt.Errorf("layout.%s (%s) and layout.%s (%s) overlap: each kind of record needs a folder of its own", f.name, f.dir, other, dir)
			}
		}
		seen[f.name] = clean
	}
	return nil
}

// SetLayout points the store at a layout, filling blanks with defaults.
func (s *Store) SetLayout(l Layout) error {
	l = l.withDefaults()
	if err := l.Validate(); err != nil {
		return err
	}
	s.layout = l
	return nil
}

// Layout is the layout in use.
func (s *Store) Layout() Layout { return s.layout }

// FilesDir is where generated PDFs and XML are written.
func (s *Store) FilesDir() string { return filepath.Join(s.root, s.layout.Files) }

// Stray finds records sitting where the default layout would keep them while
// the layout in use points elsewhere: the sign of a layout changed after
// records were written. mavis does not move files; it says so instead of
// quietly showing an empty list.
func (s *Store) Stray() []string {
	var out []string
	d := DefaultLayout()
	for i, f := range s.layout.fields() {
		def := d.fields()[i].dir
		if filepath.Clean(f.dir) == def {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(s.root, def))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				out = append(out, fmt.Sprintf("%s/ has records, but layout.%s is now %s; move them there, or set it back", def, f.name, f.dir))
				break
			}
		}
	}
	return out
}
