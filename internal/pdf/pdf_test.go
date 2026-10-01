package pdf

import (
	"bytes"
	"testing"

	"github.com/JustSteveKing/mavis/internal/store"
)

func sample(status string) store.Invoice {
	inv := store.Invoice{
		Slug: "draft-acme-2026-10", Status: status, Client: "acme", Currency: "GBP", VATTreatment: "standard", Period: "2026-10",
		Lines:    []store.Line{{Description: "Reporting module, October 2026", Qty: "5", Unit: "day", Price: 65000, VAT: 2000, Amount: 325000}},
		VATLines: []store.VATLine{{Rate: 2000, Net: 325000, VAT: 65000}},
		Net:      325000, VAT: 65000, Total: 390000,
	}
	if status != "draft" {
		inv.Number, inv.Issued, inv.TaxPoint, inv.Due = "INV-2026-001", "2026-10-31", "2026-10-31", "2026-11-30"
		inv.From = store.Issuer{Name: "Steve Ltd", Address: []string{"1 My Street"}, VATNumber: "GB999999973"}
		inv.ToName, inv.ToAddress = "Acme Ltd", []string{"1 High Street"}
	}
	return inv
}

func TestIssuedInvoiceCarriesWhatAVATInvoiceMust(t *testing.T) {
	data, err := Render(sample("issued"), Options{Uncompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	for _, want := range []string{
		"INV-2026-001", "Steve Ltd", "VAT GB999999973", "Acme Ltd", "1 High Street",
		"31 October 2026", "Reporting module, October 2026", "5 day", "650.00",
		"VAT at 20%", "3,900.00", "Total GBP", "Payment is due by 30 November 2026.",
		"\xa3", // the pound sign, in the core fonts' Windows-1252
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestADraftCannotPassForAnInvoice(t *testing.T) {
	data, err := Render(sample("draft"), Options{Uncompressed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("DRAFT INVOICE")) || !bytes.Contains(data, []byte("not yet issued")) {
		t.Fatal("a draft must say it is a draft")
	}
}

func TestReverseChargeNoteIsPrinted(t *testing.T) {
	inv := sample("issued")
	inv.VATNote = "Reverse charge applies."
	data, _ := Render(inv, Options{Uncompressed: true})
	if !bytes.Contains(data, []byte("Reverse charge applies.")) {
		t.Fatal("missing the VAT note")
	}
}

func TestCreditNoteSaysWhatItCredits(t *testing.T) {
	inv := sample("issued")
	inv.Kind, inv.Number, inv.Credits, inv.Due = "credit", "CN-2026-001", "INV-2026-001", ""
	data, _ := Render(inv, Options{Uncompressed: true})
	for _, want := range []string{"CREDIT NOTE", "CN-2026-001, crediting invoice INV-2026-001", "reduces the amount owed on invoice INV-2026-001"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("missing %q", want)
		}
	}
	if bytes.Contains(data, []byte("Payment is due")) {
		t.Error("a credit note asks for no payment")
	}
}
