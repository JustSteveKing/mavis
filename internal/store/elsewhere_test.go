package store

import (
	"strings"
	"testing"
)

func TestClientsInvoicedElsewhereGetNoDrafts(t *testing.T) {
	s := billingStore(t)
	where := "FreeAgent"
	c, err := s.SetClient("acme", ClientUpdate{Invoicing: &where})
	if err != nil || !c.InvoicedElsewhere() || c.Invoicing != "FreeAgent" {
		t.Fatalf("%+v %v", c, err)
	}
	_, _, err = s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"})
	if err == nil || !strings.Contains(err.Error(), "acme is invoiced in FreeAgent") {
		t.Fatalf("got %v", err)
	}
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Workshop", Price: "100"}}}); err == nil {
		t.Error("a hand-written line got past it")
	}
	if due, err := s.RetainersDue(); err != nil || len(due) != 0 {
		t.Errorf("retainers to bill: %+v %v", due, err)
	}

	back := " Mavis "
	if c, _ = s.SetClient("acme", ClientUpdate{Invoicing: &back}); c.InvoicedElsewhere() || c.Invoicing != "" {
		t.Fatalf("mavis should clear it: %+v", c)
	}
	if _, _, err := s.AddInvoice(NewInvoice{Client: "acme", Month: "2026-10"}); err != nil {
		t.Fatalf("cleared, but still refused: %v", err)
	}
}
