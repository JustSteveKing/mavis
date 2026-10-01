// Package ubl writes an issued invoice or credit note as a Peppol BIS
// Billing 3.0 UBL 2.1 document.
//
// The mapping follows einvoicing.dev's converter (UblWriter in
// api.einvoicing.dev), so the same invoice comes out the same from either.
// Element order follows the UBL 2.1 schema, which is strict about sequence;
// the tests validate every document against the official XSDs.
//
// Before writing, Check reports what the document would lack, in Peppol's
// own rule ids where there is one. A document that would be rejected is not
// written: an e-invoice is either right or not sent.
package ubl

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/store"
)

const (
	customization = "urn:cen.eu:en16931:2017#compliant#urn:fdc:peppol.eu:2017:poacc:billing:3.0"
	profile       = "urn:fdc:peppol.eu:2017:poacc:billing:01:1.0"
	nsCAC         = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
	nsCBC         = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2"

	// ReverseChargeCode is the VATEX code for a reverse charge exemption.
	ReverseChargeCode = "VATEX-EU-AE"
)

// Missing is one thing standing between an invoice and a valid e-invoice.
type Missing struct {
	Rule string `json:"rule,omitempty"` // the Peppol or EN 16931 rule, where there is one
	What string `json:"what"`
	Fix  string `json:"fix"`
}

func (m Missing) String() string {
	s := m.What
	if m.Rule != "" {
		s += " (" + m.Rule + ")"
	}
	return s + ": " + m.Fix
}

var peppolID = regexp.MustCompile(`^(\d{4}):(\S+)$`)

// Check lists what an invoice would need before it can be written. An empty
// list means it can be.
func Check(inv store.Invoice) []Missing {
	var out []Missing
	if inv.Status == "draft" {
		return []Missing{{What: "a number", Fix: "issue it first"}}
	}
	if !peppolID.MatchString(inv.From.PeppolID) {
		out = append(out, Missing{"PEPPOL-EN16931-R020", "your Peppol ID", "set business.peppol_id in the config, e.g. 9932:GB123456789, then regenerate"})
	}
	if !peppolID.MatchString(inv.ToPeppolID) {
		out = append(out, Missing{"PEPPOL-EN16931-R010", inv.Client + "'s Peppol ID", "mavis client set " + inv.Client + " --peppol-id 9932:GB..."})
	}
	if inv.BuyerReference == "" {
		out = append(out, Missing{"PEPPOL-EN16931-R003", "a buyer reference", "mavis client set " + inv.Client + " --buyer-reference <their PO or reference>"})
	}
	standard := false
	for _, l := range inv.Lines {
		if category(inv, l) == "S" {
			standard = true
		}
	}
	if standard && inv.From.VATNumber == "" {
		out = append(out, Missing{"BR-S-02", "your VAT number", "set business.vat_number in the config"})
	}
	if inv.VATTreatment == "reverse-charge" {
		if inv.From.VATNumber == "" || inv.ToVATNumber == "" {
			out = append(out, Missing{"BR-AE-02", "both VAT numbers, for reverse charge", "mavis client set " + inv.Client + " --vat-number ..."})
		}
		if inv.VATNote == "" {
			out = append(out, Missing{"BR-AE-10", "a reverse charge reason", "set invoicing.reverse_charge_note in the config"})
		}
	}
	return out
}

// category is the EN 16931 VAT category of a line: reverse charge for a
// reverse-charged invoice, standard for a rate above zero, zero rated
// otherwise.
func category(inv store.Invoice, l store.Line) string {
	switch {
	case inv.VATTreatment == "reverse-charge":
		return "AE"
	case l.VAT > 0:
		return "S"
	}
	return "Z"
}

// Write renders the document. preceding is the invoice a credit note
// corrects, for its issue date; nil for an invoice. It refuses a document
// Check finds wanting.
func Write(inv store.Invoice, preceding *store.Invoice) ([]byte, error) {
	if missing := Check(inv); len(missing) > 0 {
		return nil, fmt.Errorf("%s is not ready to send as an e-invoice: %d thing(s) missing", inv.Number, len(missing))
	}
	credit := inv.Kind == "credit"
	rootName := "Invoice"
	if credit {
		rootName = "CreditNote"
	}
	root := &el{name: rootName, attrs: [][2]string{
		{"xmlns", "urn:oasis:names:specification:ubl:schema:xsd:" + rootName + "-2"},
		{"xmlns:cac", nsCAC},
		{"xmlns:cbc", nsCBC},
	}}

	root.cbc("CustomizationID", customization)
	root.cbc("ProfileID", profile)
	root.cbc("ID", inv.Number)
	root.cbc("IssueDate", inv.Issued)
	if !credit && inv.Due != "" {
		root.cbc("DueDate", inv.Due)
	}
	if credit {
		root.cbc("CreditNoteTypeCode", "381")
	} else {
		root.cbc("InvoiceTypeCode", "380")
	}
	root.cbc("DocumentCurrencyCode", inv.Currency)
	root.cbc("BuyerReference", inv.BuyerReference)
	if credit {
		ref := root.cac("BillingReference").cac("InvoiceDocumentReference")
		ref.cbc("ID", inv.Credits)
		if preceding != nil && preceding.Issued != "" {
			ref.cbc("IssueDate", preceding.Issued)
		}
	}

	from := inv.From
	country := from.Country
	if country == "" {
		country = "GB"
	}
	party(root.cac("AccountingSupplierParty"), from.PeppolID, from.Name, from.Address, country, from.VATNumber)
	toCountry := inv.ToCountry
	if toCountry == "" {
		toCountry = "GB"
	}
	toName := inv.ToName
	if toName == "" {
		toName = inv.Client
	}
	party(root.cac("AccountingCustomerParty"), inv.ToPeppolID, toName, inv.ToAddress, toCountry, inv.ToVATNumber)

	// Tax, grouped by category and rate, rounded once per group as the
	// store's totals already are.
	type group struct {
		cat  string
		rate int
		net  money.Pence
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, l := range inv.Lines {
		cat := category(inv, l)
		key := fmt.Sprintf("%s/%d", cat, l.VAT)
		g := byKey[key]
		if g == nil {
			g = &group{cat: cat, rate: l.VAT}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.net += l.Amount
	}
	tax := root.cac("TaxTotal")
	tax.amount("TaxAmount", inv.VAT, inv.Currency)
	for _, g := range groups {
		sub := tax.cac("TaxSubtotal")
		sub.amount("TaxableAmount", g.net, inv.Currency)
		sub.amount("TaxAmount", g.net.MulDiv(int64(g.rate), 10000), inv.Currency)
		c := sub.cac("TaxCategory")
		c.cbc("ID", g.cat)
		c.cbc("Percent", percent(g.rate))
		if g.cat == "AE" {
			c.cbc("TaxExemptionReasonCode", ReverseChargeCode)
			c.cbc("TaxExemptionReason", inv.VATNote)
		}
		c.cac("TaxScheme").cbc("ID", "VAT")
	}

	totals := root.cac("LegalMonetaryTotal")
	totals.amount("LineExtensionAmount", inv.Net, inv.Currency)
	totals.amount("TaxExclusiveAmount", inv.Net, inv.Currency)
	totals.amount("TaxInclusiveAmount", inv.Total, inv.Currency)
	totals.amount("PayableAmount", inv.Total, inv.Currency)

	lineName, qtyName := "InvoiceLine", "InvoicedQuantity"
	if credit {
		lineName, qtyName = "CreditNoteLine", "CreditedQuantity"
	}
	for i, l := range inv.Lines {
		line := root.cac(lineName)
		line.cbc("ID", fmt.Sprint(i+1))
		line.cbc(qtyName, l.Qty).attrs = [][2]string{{"unitCode", UnitCode(l.Unit)}}
		line.amount("LineExtensionAmount", l.Amount, inv.Currency)
		item := line.cac("Item")
		item.cbc("Name", l.Description)
		c := item.cac("ClassifiedTaxCategory")
		c.cbc("ID", category(inv, l))
		c.cbc("Percent", percent(l.VAT))
		c.cac("TaxScheme").cbc("ID", "VAT")
		line.cac("Price").amount("PriceAmount", l.Price, inv.Currency)
	}

	var b bytes.Buffer
	b.WriteString(xml.Header)
	root.write(&b, 0)
	return b.Bytes(), nil
}

// party writes a supplier or customer.
func party(parent *el, endpoint, name string, address []string, country, vatNumber string) {
	p := parent.cac("Party")
	m := peppolID.FindStringSubmatch(endpoint)
	p.cbc("EndpointID", m[2]).attrs = [][2]string{{"schemeID", m[1]}}

	a := Split(address, country)
	addr := p.cac("PostalAddress")
	if a.Street != "" {
		addr.cbc("StreetName", a.Street)
	}
	if a.Additional != "" {
		addr.cbc("AdditionalStreetName", a.Additional)
	}
	if a.City != "" {
		addr.cbc("CityName", a.City)
	}
	if a.Postcode != "" {
		addr.cbc("PostalZone", a.Postcode)
	}
	if a.Line != "" {
		addr.cac("AddressLine").cbc("Line", a.Line)
	}
	addr.cac("Country").cbc("IdentificationCode", country)

	if vatNumber != "" {
		ts := p.cac("PartyTaxScheme")
		ts.cbc("CompanyID", vatNumber)
		ts.cac("TaxScheme").cbc("ID", "VAT")
	}
	p.cac("PartyLegalEntity").cbc("RegistrationName", name)
}

// Address is a free-form address split into UBL's parts.
type Address struct {
	Street, Additional, City, Postcode, Line string
}

var ukPostcode = regexp.MustCompile(`(?i)\s*\b([A-Z]{1,2}\d[A-Z\d]?\s*\d[A-Z]{2})$`)

// Split maps address lines onto UBL's parts. For a GB address ending in a
// postcode, the postcode becomes PostalZone and the rest of that line, or
// failing that the line before it, the city. Everything else is kept in
// order as street, additional street, and one free address line, so
// nothing typed is lost.
func Split(lines []string, country string) Address {
	var clean []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			clean = append(clean, l)
		}
	}
	var a Address
	if country == "GB" && len(clean) > 0 {
		last := clean[len(clean)-1]
		if m := ukPostcode.FindStringSubmatchIndex(last); m != nil {
			a.Postcode = strings.ToUpper(last[m[2]:m[3]])
			rest := strings.TrimRight(strings.TrimSpace(last[:m[0]]), ",")
			clean = clean[:len(clean)-1]
			if rest != "" {
				a.City = rest
			} else if len(clean) > 1 {
				a.City = clean[len(clean)-1]
				clean = clean[:len(clean)-1]
			}
		}
	}
	if len(clean) > 0 {
		a.Street = clean[0]
	}
	if len(clean) > 1 {
		a.Additional = clean[1]
	}
	if len(clean) > 2 {
		a.Line = strings.Join(clean[2:], ", ")
	}
	return a
}

// UnitCode maps mavis's units onto UN/ECE Recommendation 20: day, hour and
// month, and C62 ("one") for anything else.
func UnitCode(unit string) string {
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(unit), "s")) {
	case "day":
		return "DAY"
	case "hour":
		return "HUR"
	case "month":
		return "MON"
	}
	return "C62"
}

func percent(bp int) string {
	return strings.TrimRight(strings.TrimRight(money.Pence(bp).String(), "0"), ".")
}

// el is a minimal ordered XML element. encoding/xml's struct tags would
// work, but the schema's sequence is the thing most worth seeing in the
// code, and building in order shows it.
type el struct {
	name     string
	attrs    [][2]string
	text     string
	children []*el
}

func (e *el) add(name, text string) *el {
	c := &el{name: name, text: text}
	e.children = append(e.children, c)
	return c
}

func (e *el) cbc(name, text string) *el { return e.add("cbc:"+name, text) }
func (e *el) cac(name string) *el       { return e.add("cac:"+name, "") }

func (e *el) amount(name string, p money.Pence, currency string) {
	e.cbc(name, p.String()).attrs = [][2]string{{"currencyID", currency}}
}

func (e *el) write(b *bytes.Buffer, depth int) {
	indent := strings.Repeat("    ", depth)
	b.WriteString(indent + "<" + e.name)
	for _, a := range e.attrs {
		b.WriteString(" " + a[0] + `="`)
		xml.EscapeText(b, []byte(a[1]))
		b.WriteString(`"`)
	}
	switch {
	case len(e.children) > 0:
		b.WriteString(">\n")
		for _, c := range e.children {
			c.write(b, depth+1)
		}
		b.WriteString(indent + "</" + e.name + ">\n")
	default:
		b.WriteString(">")
		xml.EscapeText(b, []byte(e.text))
		b.WriteString("</" + e.name + ">\n")
	}
}
