// Package config loads ~/.config/mavis/config.yaml.
//
// The file is optional. Without it mavis still runs as long as a root is
// given by flag or by $MAVIS_ROOT.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JustSteveKing/mavis/internal/store"
	"gopkg.in/yaml.v3"
)

// Thresholds are the quiet periods, in days, behind the nudges in `today`.
type Thresholds struct {
	// ActiveQuiet: an active client with no active engagement and no contact
	// for this long is offered a move to warm.
	ActiveQuiet int `yaml:"active_quiet"`
	// WarmKeepInTouch: a warm client this quiet is listed to get in touch.
	WarmKeepInTouch int `yaml:"warm_keep_in_touch"`
	// WarmToCold: a warm client this quiet is offered a move to cold.
	WarmToCold int `yaml:"warm_to_cold"`
}

// Business is you, as an invoice names you. Nothing here is needed until
// the first invoice is issued.
type Business struct {
	Name      string   `yaml:"name"`
	Address   []string `yaml:"address"`
	Country   string   `yaml:"country"` // two letters; GB unless set
	VATNumber string   `yaml:"vat_number"`
	Email     string   `yaml:"email"`
	// PeppolID is how the Peppol network addresses you, scheme:value, such
	// as 9932:GB123456789. Only needed for e-invoices, and never derived from
	// the VAT number: 9932:GB123456789 and 9932:123456789 are different
	// participants.
	PeppolID string `yaml:"peppol_id"`
}

type Invoicing struct {
	// ReverseChargeNote is printed on invoices to overseas business clients.
	// The default is a placeholder: check it against how your accountant or
	// FreeAgent words it before issuing a real invoice.
	ReverseChargeNote string `yaml:"reverse_charge_note"`

	// StatutoryNotice adds a paragraph to the final payment reminder citing
	// the Late Payment of Commercial Debts (Interest) Act 1998. Off unless
	// set: it changes the tone with a client, so it is your call.
	StatutoryNotice bool `yaml:"statutory_notice"`
}

const DefaultReverseChargeNote = "Reverse charge: the customer is to account for any VAT due."

type Config struct {
	Root       string       `yaml:"root"`
	Business   Business     `yaml:"business"`
	Layout     store.Layout `yaml:"layout"`
	Invoicing  Invoicing    `yaml:"invoicing"`
	Thresholds Thresholds   `yaml:"thresholds"`
	// DayHours is the length of a working day, for converting between days
	// and hours. 7.5 unless set.
	DayHours float64 `yaml:"day_hours"`

	path string
}

func defaults() Config {
	return Config{
		Thresholds: Thresholds{ActiveQuiet: 14, WarmKeepInTouch: 30, WarmToCold: 60},
		DayHours:   7.5,
		Invoicing:  Invoicing{ReverseChargeNote: DefaultReverseChargeNote},
	}
}

// Path is where the config file lives: $XDG_CONFIG_HOME/mavis/config.yaml,
// falling back to ~/.config.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "mavis", "config.yaml"), nil
}

// Load reads the config file, filling anything it leaves out with defaults.
// A missing file is not an error.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	c := defaults()
	c.path = path

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if c.Business.Country == "" {
		c.Business.Country = "GB"
	}
	c.Business.Country = strings.ToUpper(c.Business.Country)
	if c.Invoicing.ReverseChargeNote == "" {
		c.Invoicing.ReverseChargeNote = DefaultReverseChargeNote
	}
	if c.DayHours <= 0 || c.DayHours > 24 {
		c.DayHours = defaults().DayHours
	}
	d := defaults().Thresholds
	if c.Thresholds.ActiveQuiet <= 0 {
		c.Thresholds.ActiveQuiet = d.ActiveQuiet
	}
	if c.Thresholds.WarmKeepInTouch <= 0 {
		c.Thresholds.WarmKeepInTouch = d.WarmKeepInTouch
	}
	if c.Thresholds.WarmToCold <= 0 {
		c.Thresholds.WarmToCold = d.WarmToCold
	}
	return &c, nil
}

// DayMinutes is the working day in whole minutes.
func (c *Config) DayMinutes() int { return int(c.DayHours*60 + 0.5) }

// ResolveRoot picks the records directory: the flag, then $MAVIS_ROOT, then
// the config file. The result is absolute.
func (c *Config) ResolveRoot(flag string) (string, error) {
	root := flag
	if root == "" {
		root = os.Getenv("MAVIS_ROOT")
	}
	if root == "" {
		root = c.Root
	}
	if root == "" {
		return "", errors.New("no records directory: run `mavis init` in it, or pass --root, or set MAVIS_ROOT")
	}
	return filepath.Abs(root)
}

// SaveRoot records root in the config file, creating it if needed. Anything
// else already in the file is kept.
func (c *Config) SaveRoot(root string) error {
	var doc map[string]any
	if data, err := os.ReadFile(c.path); err == nil {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: %w", c.path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	doc["root"] = root

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(c.path, out, 0o644); err != nil {
		return err
	}
	c.Root = root
	return nil
}

// File is the path this config was loaded from, or would be written to.
func (c *Config) File() string { return c.path }
