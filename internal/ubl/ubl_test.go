package ubl

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JustSteveKing/mavis/internal/store"
)

func ready() store.Invoice {
	return store.Invoice{
		Number: "INV-2026-001", Kind: "invoice", Status: "issued", Client: "acme",
		Currency: "GBP", VATTreatment: "standard",
		Issued: "2026-10-31", TaxPoint: "2026-10-31", Due: "2026-11-30",
		From: store.Issuer{
			Name: "Steve Ltd", Address: []string{"1 My Street", "Leeds LS1 1AA"}, Country: "GB",
			VATNumber: "GB999999973", PeppolID: "9932:GB999999973",
		},
		ToName: "Acme Ltd & Sons", ToAddress: []string{"Unit 4", "Trafford Park", "Manchester", "M17 1AA"},
		ToCountry: "GB", ToVATNumber: "GB123456789", ToPeppolID: "9932:GB123456789",
		BuyerReference: "PO-4471",
		Lines: []store.Line{
			{Description: "Reporting module, October 2026", Qty: "5", Unit: "day", Price: 65000, VAT: 2000, Amount: 325000},
			{Description: "Bug fixes, October 2026", Qty: "2.75", Unit: "hour", Price: 9000, VAT: 2000, Amount: 24750},
			{Description: "Train fare", Qty: "1", Price: 8450, VAT: 0, Amount: 8450},
		},
		Net: 358200, VAT: 69950, Total: 428150,
	}
}

func TestCheckNamesEachGapWithItsRule(t *testing.T) {
	inv := ready()
	if m := Check(inv); len(m) != 0 {
		t.Fatalf("a ready invoice should pass: %v", m)
	}

	inv.From.PeppolID, inv.ToPeppolID, inv.BuyerReference = "", "GB123", ""
	got := map[string]bool{}
	for _, m := range Check(inv) {
		got[m.Rule] = true
	}
	for _, rule := range []string{"PEPPOL-EN16931-R020", "PEPPOL-EN16931-R010", "PEPPOL-EN16931-R003"} {
		if !got[rule] {
			t.Errorf("missing %s in %v", rule, got)
		}
	}

	rc := ready()
	rc.VATTreatment, rc.ToVATNumber, rc.VATNote = "reverse-charge", "", ""
	got = map[string]bool{}
	for _, m := range Check(rc) {
		got[m.Rule] = true
	}
	if !got["BR-AE-02"] || !got["BR-AE-10"] {
		t.Errorf("reverse charge gaps: %v", got)
	}

	if _, err := Write(rc, nil); err == nil {
		t.Error("a document that would be rejected must not be written")
	}
}

func TestInvoiceShape(t *testing.T) {
	data, err := Write(ready(), nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, want := range []string{
		`<cbc:CustomizationID>urn:cen.eu:en16931:2017#compliant#urn:fdc:peppol.eu:2017:poacc:billing:3.0</cbc:CustomizationID>`,
		`<cbc:InvoiceTypeCode>380</cbc:InvoiceTypeCode>`,
		`<cbc:DueDate>2026-11-30</cbc:DueDate>`,
		`<cbc:BuyerReference>PO-4471</cbc:BuyerReference>`,
		`<cbc:EndpointID schemeID="9932">GB999999973</cbc:EndpointID>`,
		`<cbc:RegistrationName>Acme Ltd &amp; Sons</cbc:RegistrationName>`,
		`<cbc:CityName>Manchester</cbc:CityName>`,
		`<cbc:PostalZone>M17 1AA</cbc:PostalZone>`,
		`<cbc:InvoicedQuantity unitCode="DAY">5</cbc:InvoicedQuantity>`,
		`<cbc:InvoicedQuantity unitCode="HUR">2.75</cbc:InvoicedQuantity>`,
		`<cbc:InvoicedQuantity unitCode="C62">1</cbc:InvoicedQuantity>`,
		`<cbc:TaxAmount currencyID="GBP">699.50</cbc:TaxAmount>`,
		`<cbc:PayableAmount currencyID="GBP">4281.50</cbc:PayableAmount>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s", want)
		}
	}
	// Standard and zero-rated lines are separate VAT groups.
	if strings.Count(doc, "<cac:TaxSubtotal>") != 2 || !strings.Contains(doc, "<cbc:ID>Z</cbc:ID>") {
		t.Errorf("VAT groups:\n%s", doc)
	}
	validate(t, "Invoice", data)
}

func TestReverseChargeCarriesItsExemption(t *testing.T) {
	inv := ready()
	inv.VATTreatment, inv.ToCountry, inv.ToVATNumber, inv.ToPeppolID = "reverse-charge", "DE", "DE123456789", "9930:DE123456789"
	inv.ToAddress = []string{"Hauptstrasse 1", "10115 Berlin"}
	inv.VATNote = "Reverse charge: the customer is to account for any VAT due."
	for i := range inv.Lines {
		inv.Lines[i].VAT = 0
	}
	inv.VAT, inv.Total = 0, inv.Net
	data, err := Write(inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, want := range []string{"<cbc:ID>AE</cbc:ID>", "<cbc:TaxExemptionReasonCode>VATEX-EU-AE</cbc:TaxExemptionReasonCode>", "<cbc:IdentificationCode>DE</cbc:IdentificationCode>", "<cbc:StreetName>Hauptstrasse 1</cbc:StreetName>"} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s", want)
		}
	}
	customer := doc[strings.Index(doc, "<cac:AccountingCustomerParty>"):strings.Index(doc, "</cac:AccountingCustomerParty>")]
	if strings.Contains(customer, "<cbc:PostalZone>") {
		t.Error("no UK postcode parsing for a German address")
	}
	validate(t, "Invoice", data)
}

func TestCreditNoteReferencesItsInvoice(t *testing.T) {
	inv := ready()
	inv.Kind, inv.Number, inv.Credits, inv.Due = "credit", "CN-2026-001", "INV-2026-001", ""
	original := ready()
	data, err := Write(inv, &original)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, want := range []string{"<CreditNote ", "<cbc:CreditNoteTypeCode>381</cbc:CreditNoteTypeCode>", "<cac:InvoiceDocumentReference>", "<cbc:ID>INV-2026-001</cbc:ID>", "<cbc:IssueDate>2026-10-31</cbc:IssueDate>", "<cac:CreditNoteLine>", "<cbc:CreditedQuantity "} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s", want)
		}
	}
	validate(t, "CreditNote", data)
}

func TestSplit(t *testing.T) {
	for name, c := range map[string]struct {
		lines   []string
		country string
		want    Address
	}{
		"postcode on its own line": {[]string{"1 High Street", "Manchester", "M1 1AA"}, "GB", Address{Street: "1 High Street", City: "Manchester", Postcode: "M1 1AA"}},
		"city and postcode":        {[]string{"1 High Street", "Manchester M1 1AA"}, "GB", Address{Street: "1 High Street", City: "Manchester", Postcode: "M1 1AA"}},
		"lower case, comma":        {[]string{"1 High St", "leeds, ls1 1aa"}, "GB", Address{Street: "1 High St", City: "leeds", Postcode: "LS1 1AA"}},
		"long address":             {[]string{"Unit 4", "Trafford Park", "Village Way", "Manchester", "M17 1AA"}, "GB", Address{Street: "Unit 4", Additional: "Trafford Park", Line: "Village Way", City: "Manchester", Postcode: "M17 1AA"}},
		"no postcode":              {[]string{"1 High Street", "Manchester"}, "GB", Address{Street: "1 High Street", Additional: "Manchester"}},
		"abroad":                   {[]string{"Hauptstrasse 1", "10115 Berlin"}, "DE", Address{Street: "Hauptstrasse 1", Additional: "10115 Berlin"}},
	} {
		if got := Split(c.lines, c.country); got != c.want {
			t.Errorf("%s: got %+v, want %+v", name, got, c.want)
		}
	}
}

// validate checks a document against the official UBL 2.1 schema with
// xmllint, when it is installed. The schemas are vendored in testdata.
func validate(t *testing.T, root string, data []byte) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		// CI sets this, so a runner without xmllint fails rather than
		// quietly validating nothing.
		if os.Getenv("MAVIS_REQUIRE_XMLLINT") != "" {
			t.Fatal("xmllint is required here and not installed")
		}
		t.Log("xmllint not installed; skipping schema validation")
		return
	}
	path := filepath.Join(t.TempDir(), "doc.xml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	xsd := filepath.Join("testdata", "xsd", "ubl-2.1", "maindoc", "UBL-"+root+"-2.1.xsd")
	var stderr bytes.Buffer
	cmd := exec.Command(xmllint, "--noout", "--schema", xsd, path)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("does not validate against %s:\n%s\n%s", xsd, stderr.String(), data)
	}
}
