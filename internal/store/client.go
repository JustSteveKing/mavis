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

	// Billing details. None is needed until the first invoice.
	Address      []string `json:"address,omitempty"`
	Country      string   `json:"country,omitempty"`
	VATNumber    string   `json:"vat_number,omitempty"`
	VATTreatment string   `json:"vat_treatment,omitempty"` // as set; see Treatment

	// For e-invoices: the client's Peppol participant (scheme:value), and the
	// buyer reference Peppol requires on every invoice (PEPPOL-EN16931-R003),
	// often a purchase order or a cost centre they give you.
	PeppolID       string `json:"peppol_id,omitempty"`
	BuyerReference string `json:"buyer_reference,omitempty"`

	// Invoicing names where this client is invoiced when it is not mavis:
	// FreeAgent, Upwork. mavis then drafts no invoices for them, so two
	// systems never number the same client's work. Empty means mavis.
	Invoicing string `json:"invoicing,omitempty"`

	Path string `json:"path"`
}

// VATTreatments are how a client's invoices are taxed.
var VATTreatments = []string{"standard", "reverse-charge", "zero"}

// Treatment is the VAT treatment for this client's invoices: what is set,
// or standard for a UK client and reverse charge for anyone else.
func (c Client) Treatment() string {
	if c.VATTreatment != "" {
		return c.VATTreatment
	}
	if c.Country == "" || c.Country == "GB" {
		return "standard"
	}
	return "reverse-charge"
}

// InvoicedElsewhere says whether another system invoices this client.
func (c Client) InvoicedElsewhere() bool {
	return c.Invoicing != "" && !strings.EqualFold(c.Invoicing, "mavis")
}

// ErrInvoicedElsewhere is why a draft was refused for such a client.
func (c Client) errInvoicedElsewhere() error {
	return fmt.Errorf("%s is invoiced in %s (invoicing: on its note), so mavis does not draft its invoices; if that changes, mavis client set %s --invoicing mavis", c.Slug, c.Invoicing, c.Slug)
}

// NewClient is what `client add` supplies.
type NewClient struct {
	Slug, Name, Status, Contact, Email, Phone string
}

func (s *Store) clientPath(slug string) string {
	return filepath.Join(s.root, s.layout.Clients, slug+".md")
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

		Address:      d.List("address"),
		Country:      strings.ToUpper(d.Get("country")),
		VATNumber:    d.Get("vat_number"),
		VATTreatment: d.Get("vat_treatment"),

		PeppolID:       d.Get("peppol_id"),
		BuyerReference: d.Get("buyer_reference"),
		Invoicing:      d.Get("invoicing"),

		Path: path,
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
	dir := filepath.Join(s.root, s.layout.Clients)
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
			if existing == filepath.Join(s.layout.Clients, in.Slug+".md") {
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
		s.stamp(d)

		if err := write(path, d); err != nil {
			return err
		}
		out = clientFrom(path, d)
		return nil
	})
	return out, err
}

// ResolveClient finds a client by exact slug, or by a substring of its slug
// or name that matches exactly one.
func (s *Store) ResolveClient(query string) (Client, error) {
	clients, _, err := s.Clients()
	if err != nil {
		return Client{}, err
	}
	return resolve("client", query, clients,
		func(c Client) string { return c.Slug },
		func(c Client) string { return c.Name })
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

// ClientUpdate changes some of a client's fields. A nil field is left alone;
// a pointer to "" removes it.
type ClientUpdate struct {
	Name, Contact, Email, Phone, Currency *string
	Country, VATNumber, VATTreatment      *string
	PeppolID, BuyerReference              *string
	Invoicing                             *string // "" or "mavis" clears it
	TermsDays                             *int
	Address                               []string // nil leaves it alone
}

// PeppolIDPattern is scheme:value, the scheme being a four-digit code from
// the EAS list.
var PeppolIDPattern = regexp.MustCompile(`^\d{4}:\S+$`)

var (
	countryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// SetClient applies an update to a client's note, keeping everything else
// in the file as it was.
func (s *Store) SetClient(query string, u ClientUpdate) (Client, error) {
	upper := func(p *string) {
		if p != nil {
			*p = strings.ToUpper(strings.TrimSpace(*p))
		}
	}
	upper(u.Country)
	upper(u.Currency)
	if u.Country != nil && *u.Country != "" && !countryPattern.MatchString(*u.Country) {
		return Client{}, fmt.Errorf("country %q: use a two-letter code, like GB or DE", *u.Country)
	}
	if u.Currency != nil && !currencyPattern.MatchString(*u.Currency) {
		return Client{}, fmt.Errorf("currency %q: use a three-letter code, like GBP or EUR", *u.Currency)
	}
	if u.VATTreatment != nil && *u.VATTreatment != "" && !slices.Contains(VATTreatments, *u.VATTreatment) {
		return Client{}, fmt.Errorf("vat treatment must be one of %s", strings.Join(VATTreatments, ", "))
	}
	if u.PeppolID != nil && *u.PeppolID != "" && !PeppolIDPattern.MatchString(*u.PeppolID) {
		return Client{}, fmt.Errorf("peppol id %q: use scheme:value, like 9932:GB123456789", *u.PeppolID)
	}
	if u.Invoicing != nil {
		v := strings.Join(strings.Fields(*u.Invoicing), " ")
		if strings.EqualFold(v, "mavis") {
			v = ""
		}
		u.Invoicing = &v
	}
	if u.TermsDays != nil && *u.TermsDays <= 0 {
		return Client{}, fmt.Errorf("terms must be at least one day")
	}

	var out Client
	err := s.withLock(func() error {
		c, err := s.ResolveClient(query)
		if err != nil {
			return err
		}
		d, err := read(c.Path)
		if err != nil {
			return err
		}
		// A slice, not a map: new keys are appended in this order every
		// time, so the file does not reshuffle from one run to the next.
		for _, f := range []struct {
			key string
			v   *string
		}{
			{"name", u.Name}, {"contact", u.Contact}, {"email", u.Email}, {"phone", u.Phone},
			{"currency", u.Currency}, {"country", u.Country}, {"vat_number", u.VATNumber},
			{"vat_treatment", u.VATTreatment}, {"peppol_id", u.PeppolID}, {"buyer_reference", u.BuyerReference},
			{"invoicing", u.Invoicing},
		} {
			key, v := f.key, f.v
			switch {
			case v == nil:
			case *v == "":
				d.Delete(key)
			default:
				d.Set(key, *v)
			}
		}
		if u.TermsDays != nil {
			d.SetPlain("terms_days", strconv.Itoa(*u.TermsDays))
		}
		if u.Address != nil {
			if len(u.Address) == 0 {
				d.Delete("address")
			} else {
				d.SetList("address", u.Address)
			}
		}
		if err := write(c.Path, d); err != nil {
			return err
		}
		out = clientFrom(c.Path, d)
		return nil
	})
	return out, err
}
