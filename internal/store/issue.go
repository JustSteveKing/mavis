package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
)

// Issuer is you, as an issued invoice names you.
type Issuer struct {
	Name      string
	Address   []string
	VATNumber string
	Email     string
}

type IssueOptions struct {
	Issuer            Issuer
	ReverseChargeNote string
	Date              string // default today
	TaxPoint          string // default the issue date
}

// MissingError lists everything that has to be filled in before an invoice
// can be issued, all at once rather than one per attempt.
type MissingError struct {
	Invoice string
	Missing []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("%s cannot be issued yet; missing:\n  %s", e.Invoice, strings.Join(e.Missing, "\n  "))
}

var numberPattern = regexp.MustCompile(`^(INV|CN|Q)-(\d{4})-(\d{3,})$`)

// prefix is the number series for a kind: invoices and credit notes each
// run their own.
func prefix(kind string) string {
	if kind == "credit" {
		return "CN"
	}
	return "INV"
}

// nextNumber is one past the highest number issued in a series for a year.
// Numbers are derived from the files that exist, never kept in a counter
// that could drift from them.
func nextNumber(invoices []Invoice, series, year string) string {
	high := 0
	for _, inv := range invoices {
		m := numberPattern.FindStringSubmatch(inv.Number)
		if m != nil && m[1] == series && m[2] == year {
			if n, _ := strconv.Atoi(m[3]); n > high {
				high = n
			}
		}
	}
	return fmt.Sprintf("%s-%s-%03d", series, year, high+1)
}

// IssueInvoice numbers a draft and freezes it.
//
// Everything a VAT invoice must show is checked first, and every gap is
// reported together. The issuer's and the client's details are copied into
// the invoice, so it reads the same however either changes later. The file
// is renamed to its number.
func (s *Store) IssueInvoice(query string, o IssueOptions) (Invoice, error) {
	date, err := s.ParseDay(o.Date)
	if err != nil {
		return Invoice{}, err
	}
	taxPoint := date
	if o.TaxPoint != "" {
		if taxPoint, err = s.ParseDay(o.TaxPoint); err != nil {
			return Invoice{}, fmt.Errorf("tax point: %w", err)
		}
	}

	var out Invoice
	err = s.withLock(func() error {
		inv, err := s.ResolveInvoice(query)
		if err != nil {
			return err
		}
		if inv.Status != "draft" {
			return fmt.Errorf("%s is already %s", inv.Number, inv.Status)
		}
		client, err := s.ResolveClient(inv.Client)
		if err != nil {
			return fmt.Errorf("%s: %w", inv.Slug, err)
		}

		var missing []string
		if o.Issuer.Name == "" {
			missing = append(missing, "your name: business.name in the config")
		}
		if len(o.Issuer.Address) == 0 {
			missing = append(missing, "your address: business.address in the config")
		}
		if o.Issuer.VATNumber == "" && inv.VATTreatment != "zero" {
			missing = append(missing, "your VAT number: business.vat_number in the config")
		}
		if len(client.Address) == 0 {
			missing = append(missing, fmt.Sprintf("%s's address: mavis client set %s --address ...", client.Name, client.Slug))
		}
		if len(inv.Lines) == 0 {
			missing = append(missing, "any lines to bill")
		} else if inv.Total <= 0 {
			missing = append(missing, "a total above zero; a negative invoice is a credit note")
		}
		var credited Invoice
		if inv.Kind == "credit" {
			if credited, err = s.ResolveInvoice(inv.Credits); err != nil {
				return fmt.Errorf("%s credits %s: %w", inv.Slug, inv.Credits, err)
			}
			if left := credited.Total - credited.Credited; inv.Total > left {
				return fmt.Errorf("%s credits %s, but only %s is left on %s", inv.Slug, inv.Total.Display(), left.Display(), credited.Number)
			}
		}
		if inv.VATTreatment == "reverse-charge" && o.ReverseChargeNote == "" {
			missing = append(missing, "reverse charge wording: invoicing.reverse_charge_note in the config")
		}
		if len(missing) > 0 {
			return &MissingError{Invoice: inv.Slug, Missing: missing}
		}

		all, _, err := s.Invoices()
		if err != nil {
			return err
		}
		number := nextNumber(all, prefix(inv.Kind), date[:4])
		path := filepath.Join(s.root, InvoicesDir, number+".md")
		if existing, err := s.findNote(number); err != nil {
			return err
		} else if existing != "" {
			return fmt.Errorf("%s already exists, so %s cannot be issued as %s", existing, inv.Slug, number)
		}

		d, err := read(inv.Path)
		if err != nil {
			return err
		}
		issued, _ := time.Parse(dateLayout, date)
		due := issued.AddDate(0, 0, client.TermsDays).Format(dateLayout)

		d.Set("status", "issued")
		d.Set("number", number)
		d.SetPlain("issued", date)
		d.SetPlain("tax_point", taxPoint)
		if inv.Kind != "credit" {
			d.SetPlain("due", due)
		}
		d.Set("net", inv.Net.String())
		d.Set("vat", inv.VAT.String())
		d.Set("total", inv.Total.String())
		d.Set("from_name", o.Issuer.Name)
		d.SetList("from_address", o.Issuer.Address)
		if o.Issuer.VATNumber != "" {
			d.Set("from_vat_number", o.Issuer.VATNumber)
		}
		if o.Issuer.Email != "" {
			d.Set("from_email", o.Issuer.Email)
		}
		d.Set("to_name", client.Name)
		d.SetList("to_address", client.Address)
		if client.VATNumber != "" {
			d.Set("to_vat_number", client.VATNumber)
		}
		if inv.VATTreatment == "reverse-charge" {
			d.Set("vat_note", o.ReverseChargeNote)
		}
		d.Body = renderLines(inv.Lines)

		if err := write(path, d); err != nil {
			return err
		}
		if err := os.Remove(inv.Path); err != nil {
			return fmt.Errorf("issued as %s, but the draft could not be removed: %w", number, err)
		}
		out, err = invoiceFrom(path, d)
		return err
	})
	return out, err
}

// SetPaid marks an issued invoice paid on a date, or with paid false, takes
// that back. Paying is the only change an issued invoice accepts.
func (s *Store) SetPaid(query string, paid bool, date string) (Invoice, error) {
	day, err := s.ParseDay(date)
	if err != nil {
		return Invoice{}, err
	}
	var out Invoice
	err = s.withLock(func() error {
		inv, err := s.ResolveInvoice(query)
		if err != nil {
			return err
		}
		switch {
		case inv.Status == "draft":
			return fmt.Errorf("%s is a draft; issue it first", inv.Slug)
		case inv.Kind == "credit":
			return fmt.Errorf("%s is a credit note; nothing is paid on it", inv.Number)
		case paid && inv.Status == "paid":
			return fmt.Errorf("%s was already paid on %s", inv.Number, inv.Paid)
		case !paid && inv.Status != "paid":
			return fmt.Errorf("%s is not marked paid", inv.Number)
		}
		if paid && day < inv.Issued {
			return fmt.Errorf("%s was issued on %s, so it cannot have been paid on %s", inv.Number, inv.Issued, day)
		}
		d, err := read(inv.Path)
		if err != nil {
			return err
		}
		if paid {
			d.Set("status", "paid")
			d.SetPlain("paid", day)
		} else {
			d.Set("status", "issued")
			d.Delete("paid")
		}
		if err := write(inv.Path, d); err != nil {
			return err
		}
		out, err = invoiceFrom(inv.Path, d)
		return err
	})
	return out, err
}

// checkFrozen compares an issued invoice's lines with the totals stored when
// it was issued. A mismatch means the file was edited after issue.
func checkFrozen(inv Invoice, net, vat, total string) error {
	if inv.Status == "draft" {
		return nil
	}
	for _, f := range []struct {
		name   string
		stored string
		now    money.Pence
	}{{"net", net, inv.Net}, {"VAT", vat, inv.VAT}, {"total", total, inv.Total}} {
		p, err := money.Parse(f.stored)
		if err != nil || p != f.now {
			return errors.New("edited after it was issued: its lines no longer add up to the " + f.name + " it was issued with; correct an issued invoice with a credit note")
		}
	}
	return nil
}
