package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/record"
)

// QuotesDir holds quotes: a draft until sent, frozen after.
const QuotesDir = "quotes"

// QuoteStatuses: expired is not among them. It is derived from valid_until,
// so a quote does not have to be rewritten for time to pass.
var QuoteStatuses = []string{"draft", "sent", "accepted", "declined"}

// DefaultValidDays is how long a quote stands unless told otherwise.
const DefaultValidDays = 30

type Quote struct {
	Slug         string `json:"slug"`
	Number       string `json:"number,omitempty"`
	Status       string `json:"status"`
	Client       string `json:"client"`
	Title        string `json:"title"`
	Currency     string `json:"currency"`
	VATTreatment string `json:"vat_treatment"`
	ValidDays    int    `json:"valid_days"`
	Created      string `json:"created,omitempty"`
	Sent         string `json:"sent,omitempty"`
	ValidUntil   string `json:"valid_until,omitempty"`
	Decided      string `json:"decided,omitempty"`
	Engagement   string `json:"engagement,omitempty"` // started when it was accepted

	From        Issuer   `json:"from"`
	ToName      string   `json:"to_name,omitempty"`
	ToAddress   []string `json:"to_address,omitempty"`
	ToVATNumber string   `json:"to_vat_number,omitempty"`
	VATNote     string   `json:"vat_note,omitempty"`

	// Scope is the prose above the lines table: what the client is getting.
	Scope    string      `json:"scope,omitempty"`
	Lines    []Line      `json:"lines"`
	VATLines []VATLine   `json:"vat_lines"`
	Net      money.Pence `json:"net"`
	VAT      money.Pence `json:"vat"`
	Total    money.Pence `json:"total"`

	Path string `json:"path"`
}

// Expired reports a sent quote still waiting on an answer after its
// validity has run out.
func (q Quote) Expired(today string) bool {
	return q.Status == "sent" && q.ValidUntil != "" && q.ValidUntil < today
}

// Display is the status to show, with expiry worked out.
func (q Quote) Display(today string) string {
	if q.Expired(today) {
		return "expired"
	}
	return q.Status
}

// scopeOf is the body above the first table row.
func scopeOf(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			break
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func quoteFrom(path string, d *record.Document) (Quote, error) {
	q := Quote{
		Slug:         strings.TrimSuffix(filepath.Base(path), ".md"),
		Number:       d.Get("number"),
		Status:       d.Get("status"),
		Client:       linkTarget(d.Get("client")),
		Title:        d.Get("title"),
		Currency:     d.Get("currency"),
		VATTreatment: d.Get("vat_treatment"),
		Created:      d.Get("created"),
		Sent:         d.Get("sent"),
		ValidUntil:   d.Get("valid_until"),
		Decided:      d.Get("decided"),
		Engagement:   linkTarget(d.Get("engagement")),
		From: Issuer{
			Name:      d.Get("from_name"),
			Address:   d.List("from_address"),
			VATNumber: d.Get("from_vat_number"),
			Email:     d.Get("from_email"),
		},
		ToName:      d.Get("to_name"),
		ToAddress:   d.List("to_address"),
		ToVATNumber: d.Get("to_vat_number"),
		VATNote:     d.Get("vat_note"),
		Scope:       scopeOf(d.Body),
		Path:        path,
	}
	if q.Currency == "" {
		q.Currency = "GBP"
	}
	if q.VATTreatment == "" {
		q.VATTreatment = "standard"
	}
	q.ValidDays = DefaultValidDays
	if n, err := strconv.Atoi(d.Get("valid_days")); err == nil && n > 0 {
		q.ValidDays = n
	}
	lines, err := parseLines(d, defaultVATFor(q.VATTreatment))
	if err != nil {
		return q, err
	}
	q.Lines = lines
	q.VATLines, q.Net, q.VAT = totals(lines)
	q.Total = q.Net + q.VAT
	if q.Status != "draft" {
		// Reuses the invoice check: a sent quote's lines must still add up
		// to what was sent.
		if err := checkFrozen(Invoice{Status: q.Status, Net: q.Net, VAT: q.VAT, Total: q.Total}, d.Get("net"), d.Get("vat"), d.Get("total")); err != nil {
			return q, errors.New(strings.Replace(err.Error(), "issued", "sent", -1))
		}
	}
	return q, nil
}

// Quotes lists every quote, numbered ones first in number order.
func (s *Store) Quotes() ([]Quote, []Problem, error) {
	dir := filepath.Join(s.root, QuotesDir)
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []Quote
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
		if d.Get("type") != "quote" {
			continue
		}
		q, err := quoteFrom(path, d)
		if err != nil {
			problems = append(problems, Problem{Path: path, Err: err})
			continue
		}
		out = append(out, q)
	}
	slices.SortStableFunc(out, func(a, b Quote) int {
		switch {
		case a.Number == "" && b.Number != "":
			return 1
		case a.Number != "" && b.Number == "":
			return -1
		}
		if c := strings.Compare(a.Number, b.Number); c != 0 {
			return c
		}
		return strings.Compare(a.Slug, b.Slug)
	})
	return out, problems, nil
}

// ResolveQuote finds a quote by number, slug, or a unique substring.
func (s *Store) ResolveQuote(query string) (Quote, error) {
	all, _, err := s.Quotes()
	if err != nil {
		return Quote{}, err
	}
	for _, q := range all {
		if q.Number == query {
			return q, nil
		}
	}
	return resolve("quote", query, all,
		func(q Quote) string { return q.Slug },
		func(q Quote) string { return q.Number + " " + q.Title })
}

type NewQuote struct {
	Client, Title, Scope string
	Lines                []ManualLine
	ValidDays            int
}

// AddQuote drafts a quote. Scope is written above the lines, as prose.
func (s *Store) AddQuote(in NewQuote) (Quote, error) {
	if strings.TrimSpace(in.Title) == "" {
		return Quote{}, errors.New("a quote needs a --title: what it is for")
	}
	if in.ValidDays < 0 {
		return Quote{}, errors.New("a quote cannot be valid for less than a day")
	}
	if in.ValidDays == 0 {
		in.ValidDays = DefaultValidDays
	}
	var out Quote
	err := s.withLock(func() error {
		client, err := s.ResolveClient(in.Client)
		if err != nil {
			return err
		}
		vat := defaultVATFor(client.Treatment())
		var lines []Line
		for _, m := range in.Lines {
			l, err := manualLine(m, vat)
			if err != nil {
				return err
			}
			lines = append(lines, l)
		}

		dir := filepath.Join(s.root, QuotesDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path, err := freePath(dir, "draft-"+client.Slug)
		if err != nil {
			return err
		}
		d := record.New()
		d.Set("type", "quote")
		d.Set("status", "draft")
		d.Set("client", link(client.Slug))
		d.Set("title", strings.TrimSpace(in.Title))
		d.Set("currency", client.Currency)
		d.Set("vat_treatment", client.Treatment())
		d.SetPlain("valid_days", strconv.Itoa(in.ValidDays))
		d.SetPlain("created", s.today())
		var body strings.Builder
		if scope := strings.TrimSpace(in.Scope); scope != "" {
			body.WriteString(scope + "\n\n")
		}
		body.WriteString(renderLines(lines))
		d.Body = body.String()
		if err := write(path, d); err != nil {
			return err
		}
		out, err = quoteFrom(path, d)
		return err
	})
	return out, err
}

// manualLine builds a line from what a person typed.
func manualLine(m ManualLine, vat int) (Line, error) {
	qty := m.Qty
	if qty == "" {
		qty = "1"
	}
	q, err := money.Parse(qty)
	if err != nil || q <= 0 {
		return Line{}, fmt.Errorf("line %q: quantity %q is not a number above zero", m.Description, qty)
	}
	price, err := money.Parse(m.Price)
	if err != nil {
		return Line{}, fmt.Errorf("line %q: %w", m.Description, err)
	}
	return Line{Description: m.Description, Qty: trimQty(q), Unit: m.Unit, Price: price, VAT: vat, Amount: price.MulDiv(int64(q), 100)}, nil
}

// SendQuote numbers a draft (Q-2026-001), stamps when it was sent and how
// long it stands, snapshots both parties, and freezes it. A quote is not a
// VAT invoice, so the client's address is not required: a prospect may not
// have given one yet.
func (s *Store) SendQuote(query string, o IssueOptions) (Quote, error) {
	date, err := s.ParseDay(o.Date)
	if err != nil {
		return Quote{}, err
	}
	var out Quote
	err = s.withLock(func() error {
		q, err := s.ResolveQuote(query)
		if err != nil {
			return err
		}
		if q.Status != "draft" {
			return fmt.Errorf("%s was already sent", q.Number)
		}
		client, err := s.ResolveClient(q.Client)
		if err != nil {
			return fmt.Errorf("%s: %w", q.Slug, err)
		}
		var missing []string
		if o.Issuer.Name == "" {
			missing = append(missing, "your name: business.name in the config")
		}
		if len(q.Lines) == 0 {
			missing = append(missing, "any lines to price: mavis quote edit "+q.Slug)
		} else if q.Total <= 0 {
			missing = append(missing, "a total above zero")
		}
		if q.VATTreatment == "reverse-charge" && o.ReverseChargeNote == "" {
			missing = append(missing, "reverse charge wording: invoicing.reverse_charge_note in the config")
		}
		if len(missing) > 0 {
			return &MissingError{Invoice: q.Slug, Missing: missing}
		}

		quotes, _, err := s.Quotes()
		if err != nil {
			return err
		}
		high := 0
		for _, other := range quotes {
			m := numberPattern.FindStringSubmatch(other.Number)
			if m != nil && m[1] == "Q" && m[2] == date[:4] {
				if n, _ := strconv.Atoi(m[3]); n > high {
					high = n
				}
			}
		}
		number := fmt.Sprintf("Q-%s-%03d", date[:4], high+1)
		path := filepath.Join(s.root, QuotesDir, number+".md")
		if existing, err := s.findNote(number); err != nil {
			return err
		} else if existing != "" {
			return fmt.Errorf("%s already exists, so %s cannot be sent as %s", existing, q.Slug, number)
		}

		d, err := read(q.Path)
		if err != nil {
			return err
		}
		sent, _ := time.Parse(dateLayout, date)
		d.Set("status", "sent")
		d.Set("number", number)
		d.SetPlain("sent", date)
		d.SetPlain("valid_until", sent.AddDate(0, 0, q.ValidDays).Format(dateLayout))
		d.Set("net", q.Net.String())
		d.Set("vat", q.VAT.String())
		d.Set("total", q.Total.String())
		d.Set("from_name", o.Issuer.Name)
		if len(o.Issuer.Address) > 0 {
			d.SetList("from_address", o.Issuer.Address)
		}
		if o.Issuer.VATNumber != "" {
			d.Set("from_vat_number", o.Issuer.VATNumber)
		}
		if o.Issuer.Email != "" {
			d.Set("from_email", o.Issuer.Email)
		}
		d.Set("to_name", client.Name)
		if len(client.Address) > 0 {
			d.SetList("to_address", client.Address)
		}
		if client.VATNumber != "" {
			d.Set("to_vat_number", client.VATNumber)
		}
		if q.VATTreatment == "reverse-charge" {
			d.Set("vat_note", o.ReverseChargeNote)
		}
		scope := ""
		if q.Scope != "" {
			scope = q.Scope + "\n\n"
		}
		d.Body = scope + renderLines(q.Lines)

		if err := write(path, d); err != nil {
			return err
		}
		if err := os.Remove(q.Path); err != nil {
			return fmt.Errorf("sent as %s, but the draft could not be removed: %w", number, err)
		}
		out, err = quoteFrom(path, d)
		return err
	})
	return out, err
}

// StartWork is how an accepted quote becomes an engagement. Name is the
// engagement's short name; with no basis it is fixed price, budgeted at the
// quote's net total.
type StartWork struct {
	Name, Basis, Rate string
}

// DecideQuote records the answer to a sent quote. Accepting can start the
// engagement the quote was for, linked both ways.
func (s *Store) DecideQuote(query string, accepted bool, date string, work *StartWork) (Quote, Engagement, error) {
	day, err := s.ParseDay(date)
	if err != nil {
		return Quote{}, Engagement{}, err
	}
	if work != nil && !accepted {
		return Quote{}, Engagement{}, errors.New("only an accepted quote starts an engagement")
	}
	var q Quote
	err = s.withLock(func() error {
		if q, err = s.ResolveQuote(query); err != nil {
			return err
		}
		switch q.Status {
		case "draft":
			return fmt.Errorf("%s is a draft; send it first", q.Slug)
		case "accepted", "declined":
			return fmt.Errorf("%s was already %s on %s", q.Number, q.Status, q.Decided)
		}
		if day < q.Sent {
			return fmt.Errorf("%s was sent on %s, so it cannot have been answered on %s", q.Number, q.Sent, day)
		}
		return nil
	})
	if err != nil {
		return q, Engagement{}, err
	}

	// The engagement is made before the quote is marked, so a failure there
	// (a name already taken, say) leaves the quote open to try again.
	var eng Engagement
	if work != nil {
		in := NewEngagement{Client: q.Client, Name: work.Name, Title: q.Title, Status: "active", Basis: work.Basis, Rate: work.Rate, Start: day, Quote: q.Number}
		if in.Basis == "" {
			in.Basis = "fixed"
		}
		if in.Basis == "fixed" {
			in.Budget = q.Net.String()
		}
		if eng, err = s.AddEngagement(in); err != nil {
			return q, Engagement{}, fmt.Errorf("%s is still open; the engagement could not be started: %w", q.Number, err)
		}
	}

	err = s.withLock(func() error {
		d, err := read(q.Path)
		if err != nil {
			return err
		}
		// The lock was let go while the engagement was made; check nothing
		// answered the quote in between.
		if now := d.Get("status"); now != "sent" {
			return fmt.Errorf("%s was answered (%s) while this was running", q.Number, now)
		}
		if accepted {
			d.Set("status", "accepted")
		} else {
			d.Set("status", "declined")
		}
		d.SetPlain("decided", day)
		if eng.Slug != "" {
			d.Set("engagement", link(eng.Slug))
		}
		if err := write(q.Path, d); err != nil {
			return err
		}
		q, err = quoteFrom(q.Path, d)
		return err
	})
	return q, eng, err
}

// DiscardQuote deletes a draft. A sent quote is a record of what was
// offered, so it is declined, never deleted.
func (s *Store) DiscardQuote(query string) (Quote, error) {
	var out Quote
	err := s.withLock(func() error {
		q, err := s.ResolveQuote(query)
		if err != nil {
			return err
		}
		if q.Status != "draft" {
			return fmt.Errorf("%s was sent; record the answer with accept or decline instead", q.Number)
		}
		out = q
		return os.Remove(q.Path)
	})
	return out, err
}
