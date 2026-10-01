package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLayoutValidation(t *testing.T) {
	for name, l := range map[string]Layout{
		"absolute":        {Clients: "/etc/clients"},
		"escapes":         {Clients: "../clients"},
		"the root itself": {Log: "."},
		"shared":          {Invoices: "billing", Quotes: "billing"},
		"nested in other": {Clients: "crm", Engagements: "crm/work"},
	} {
		if err := l.withDefaults().Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	ok := Layout{Clients: "business/clients", Log: "business/log", Files: ".mavis/files"}.withDefaults()
	if err := ok.Validate(); err != nil {
		t.Fatalf("a nested layout is fine: %v", err)
	}
	if ok.Quotes != "quotes" {
		t.Error("blanks take the default")
	}
}

func TestACustomLayoutIsUsedThroughout(t *testing.T) {
	s := newStore(t)
	if err := s.SetLayout(Layout{Clients: "business/clients", Log: "business/log", Invoices: "business/invoices", Files: ".business"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	c, err := s.AddClient(NewClient{Slug: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(s.Root(), "business", "clients", "acme.md"); c.Path != want {
		t.Fatalf("client at %s, want %s", c.Path, want)
	}
	l, _ := s.AddLog(NewLog{Kind: "note", Client: "acme", Summary: "x"})
	if !strings.Contains(l.Path, filepath.Join("business", "log")) {
		t.Fatalf("log at %s", l.Path)
	}
	inv, _, err := s.AddInvoice(NewInvoice{Client: "acme", Lines: []ManualLine{{Description: "Work", Price: "100"}}})
	if err != nil || !strings.Contains(inv.Path, filepath.Join("business", "invoices")) {
		t.Fatalf("invoice at %s: %v", inv.Path, err)
	}
	if s.FilesDir() != filepath.Join(s.Root(), ".business") {
		t.Fatalf("files dir %s", s.FilesDir())
	}
	// Listing finds them where the layout says.
	if all, _, _ := s.Clients(); len(all) != 1 {
		t.Fatalf("clients: %+v", all)
	}
	// The duplicate-name guard still covers the whole folder.
	os.WriteFile(filepath.Join(s.Root(), "globex.md"), []byte("x"), 0o644)
	if _, err := s.AddClient(NewClient{Slug: "globex"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("collision outside the layout: %v", err)
	}
}

func TestStrayRecordsAreReported(t *testing.T) {
	s := newStore(t) // default layout, with records in clients/
	s.AddClient(NewClient{Slug: "acme"})
	if got := s.Stray(); len(got) != 0 {
		t.Fatalf("nothing is stray yet: %v", got)
	}
	s.SetLayout(Layout{Clients: "crm"})
	got := s.Stray()
	if len(got) != 1 || !strings.Contains(got[0], "clients/ has records, but layout.clients is now crm") {
		t.Fatalf("stray = %v", got)
	}
}
