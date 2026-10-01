package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	return s
}

func TestAddClientWritesANoteObsidianCanRead(t *testing.T) {
	s := newStore(t)
	c, err := s.AddClient(NewClient{Slug: "acme", Name: "Acme Ltd", Contact: "Jo Bloggs", Phone: "+441610000000"})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(c.Path)
	want := `---
type: client
status: active
status_since: 2026-10-01
name: Acme Ltd
contact: Jo Bloggs
phone: "+441610000000"
currency: GBP
terms_days: 30
created: 2026-10-01
---
`
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
}

func TestAddClientRefusesDuplicatesAndAmbiguousLinks(t *testing.T) {
	s := newStore(t)
	if _, err := s.AddClient(NewClient{Slug: "acme"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddClient(NewClient{Slug: "acme"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate: err = %v", err)
	}

	// A note elsewhere in the vault with the same name.
	os.MkdirAll(filepath.Join(s.Root(), "projects"), 0o755)
	os.WriteFile(filepath.Join(s.Root(), "projects", "globex.md"), []byte("# globex\n"), 0o644)
	_, err := s.AddClient(NewClient{Slug: "globex"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("collision: err = %v", err)
	}

	// Dot-directories are not the vault, so a checkout there does not count.
	os.MkdirAll(filepath.Join(s.Root(), ".repos", "initech"), 0o755)
	os.WriteFile(filepath.Join(s.Root(), ".repos", "initech", "initech.md"), []byte("x"), 0o644)
	if _, err := s.AddClient(NewClient{Slug: "initech"}); err != nil {
		t.Fatalf("dot-directory should be ignored: %v", err)
	}
}

func TestAddClientRejectsBadSlugsWithASuggestion(t *testing.T) {
	s := newStore(t)
	_, err := s.AddClient(NewClient{Slug: "Acme Ltd"})
	if err == nil || !strings.Contains(err.Error(), `"acme-ltd"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveClient(t *testing.T) {
	s := newStore(t)
	for _, c := range []NewClient{{Slug: "acme", Name: "Acme Ltd"}, {Slug: "acme-labs"}, {Slug: "globex", Name: "Globex Corporation"}} {
		if _, err := s.AddClient(c); err != nil {
			t.Fatal(err)
		}
	}

	if c, err := s.ResolveClient("acme"); err != nil || c.Slug != "acme" {
		t.Errorf("exact slug should win over substring: %v %v", c.Slug, err)
	}
	if c, err := s.ResolveClient("corp"); err != nil || c.Slug != "globex" {
		t.Errorf("name substring: %v %v", c.Slug, err)
	}
	var amb *AmbiguousError
	if _, err := s.ResolveClient("acm"); !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Errorf("want ambiguity, got %v", err)
	}
	if _, err := s.ResolveClient("nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want not found, got %v", err)
	}
}

func TestSetClientStatusStampsAndKeepsHandEdits(t *testing.T) {
	s := newStore(t)
	c, _ := s.AddClient(NewClient{Slug: "acme"})

	// A hand edit in Obsidian: an extra property and some body text.
	data, _ := os.ReadFile(c.Path)
	edited := strings.Replace(string(data), "created:", "referred_by: Sam\ncreated:", 1) + "Met at Laracon.\n"
	os.WriteFile(c.Path, []byte(edited), 0o644)

	s.Now = func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }
	got, changed, err := s.SetClientStatus("acme", "warm")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got.Status != "warm" || got.StatusSince != "2026-11-02" {
		t.Fatalf("got %+v", got)
	}
	after, _ := os.ReadFile(c.Path)
	for _, want := range []string{"referred_by: Sam", "Met at Laracon.", "status: warm", "status_since: 2026-11-02"} {
		if !strings.Contains(string(after), want) {
			t.Errorf("missing %q in:\n%s", want, after)
		}
	}

	if _, changed, _ := s.SetClientStatus("acme", "warm"); changed {
		t.Error("same status should not count as a change")
	}
	if _, _, err := s.SetClientStatus("acme", "lukewarm"); err == nil {
		t.Error("want an error for an unknown status")
	}
}

func TestListingSkipsNonClientsAndReportsBrokenFiles(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	dir := filepath.Join(s.Root(), ClientsDir)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# How clients work\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\ntype: client\nunclosed\n"), 0o644)

	clients, problems, err := s.Clients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].Slug != "acme" {
		t.Errorf("clients = %+v", clients)
	}
	if len(problems) != 1 || !strings.HasSuffix(problems[0].Path, "broken.md") {
		t.Errorf("problems = %+v", problems)
	}
}

func TestConcurrentAddsDoNotCollide(t *testing.T) {
	s := newStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AddClient(NewClient{Slug: "acme"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	ok := 0
	for err := range errs {
		if err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d adds succeeded for one slug, want exactly 1", ok)
	}
}

func TestSetClientBillingDetails(t *testing.T) {
	s := newStore(t)
	c, _ := s.AddClient(NewClient{Slug: "acme", Phone: "+44 1"})
	os.WriteFile(c.Path, append(mustRead(t, c.Path), []byte("Met at Laracon.\n")...), 0o644)

	str := func(v string) *string { return &v }
	terms := 14
	got, err := s.SetClient("acme", ClientUpdate{
		Address: []string{"1 High Street", "Manchester", "M1 1AA"},
		Country: str("gb"), VATNumber: str("GB123456789"), TermsDays: &terms, Phone: str(""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Address) != 3 || got.Country != "GB" || got.TermsDays != 14 || got.Phone != "" || got.Treatment() != "standard" {
		t.Fatalf("got %+v", got)
	}
	data := string(mustRead(t, c.Path))
	if !strings.Contains(data, "address: [1 High Street, Manchester, M1 1AA]") || !strings.HasSuffix(data, "Met at Laracon.\n") || strings.Contains(data, "phone") {
		t.Fatalf("file:\n%s", data)
	}

	got, _ = s.SetClient("acme", ClientUpdate{Country: str("DE")})
	if got.Treatment() != "reverse-charge" {
		t.Errorf("an overseas client defaults to reverse charge, got %s", got.Treatment())
	}
	got, _ = s.SetClient("acme", ClientUpdate{VATTreatment: str("standard")})
	if got.Treatment() != "standard" {
		t.Errorf("an explicit treatment wins, got %s", got.Treatment())
	}

	for name, u := range map[string]ClientUpdate{
		"country":   {Country: str("Germany")},
		"currency":  {Currency: str("pounds")},
		"treatment": {VATTreatment: str("exempt-ish")},
	} {
		if _, err := s.SetClient("acme", u); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
