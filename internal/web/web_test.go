package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JustSteveKing/mavis/internal/store"
)

func seeded(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Init()
	s.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.AddClient(store.NewClient{Slug: "acme", Name: "Acme Ltd", Contact: "Jo Bloggs"})
	must(err)
	_, err = s.SetClient("acme", store.ClientUpdate{Address: []string{"1 High Street"}})
	must(err)
	_, err = s.AddEngagement(store.NewEngagement{Client: "acme", Name: "reporting", Title: "Reporting", Basis: "day", Rate: "650", Start: "2026-09-01"})
	must(err)
	_, err = s.AddTime(store.NewTime{Engagement: "acme-reporting", Duration: "2d", What: "Exports", Date: "2026-09-10"})
	must(err)
	_, err = s.AddLog(store.NewLog{Kind: "call", Client: "acme", Engagement: "acme-reporting",
		Summary:   "Scoped it with [[acme-reporting|the reporting work]], <script>alert(1)</script> & <b>more</b>",
		FollowUps: []store.NewFollowUp{{Text: "Send estimate", Due: "2026-09-28"}}})
	must(err)
	d, _, err := s.AddInvoice(store.NewInvoice{Client: "acme", Lines: []store.ManualLine{{Description: "Workshop", Price: "1000"}}})
	must(err)
	_, err = s.IssueInvoice(d.Slug, store.IssueOptions{Issuer: store.Issuer{Name: "Steve", Address: []string{"1 My St"}, VATNumber: "GB999999973"}, Date: "2026-08-01"})
	must(err)
	_, err = s.AddQuote(store.NewQuote{Client: "acme", Title: "Rebuild", Scope: "A **new** reporting module.", Lines: []store.ManualLine{{Description: "Build", Price: "5000"}}})
	must(err)

	h, err := New(s, Options{Quiet: store.Quiet{ActiveQuiet: 14, WarmKeepInTouch: 30, WarmToCold: 60}})
	must(err)
	return s, h
}

func get(t *testing.T, h http.Handler, path string) *http.Response {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://localhost:4000"+path, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Result()
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func logSlug(t *testing.T, s *store.Store) string {
	t.Helper()
	logs, _, err := s.Logs()
	if err != nil || len(logs) == 0 {
		t.Fatalf("no log entry: %v", err)
	}
	return logs[0].Slug
}

func TestEveryPageRenders(t *testing.T) {
	s, h := seeded(t)
	pages := map[string]string{
		"/":                           "Overdue invoices",
		"/clients/":                   "Acme Ltd",
		"/clients/acme":               "Send estimate",
		"/engagements/":               "Reporting",
		"/engagements/acme-reporting": "Exports",
		"/log/":                       "Scoped it",
		"/log/" + logSlug(t, s):       "Send estimate",
		"/follow-ups/":                "Send estimate",
		"/time/?month=2026-09":        "Exports",
		"/time/":                      "No time logged or work delivered in October 2026",
		"/invoices/":                  "INV-2026-001",
		"/invoices/INV-2026-001":      "Workshop",
		"/quotes/":                    "Rebuild",
		"/stats/?month=2026-09":       "Reporting",
		"/stats/?year=2026":           "Invoicing",
		"/stats/":                     "October 2026",
	}
	for path, want := range pages {
		res := get(t, h, path)
		got := body(t, res)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d\n%s", path, res.StatusCode, got)
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: no %q in the page", path, want)
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no Content-Security-Policy", path)
		}
	}
}

func TestQuotePage(t *testing.T) {
	s, h := seeded(t)
	quotes, _, _ := s.Quotes()
	got := body(t, get(t, h, "/quotes/"+quotes[0].Slug))
	if !strings.Contains(got, "<strong>new</strong>") {
		t.Errorf("the scope is not rendered as Markdown:\n%s", got)
	}
}

func TestRecordTextCannotScriptThePage(t *testing.T) {
	s, h := seeded(t)
	for _, path := range []string{"/log/", "/clients/acme", "/log/" + logSlug(t, s)} {
		got := body(t, get(t, h, path))
		if strings.Contains(got, "<script>") {
			t.Errorf("%s: a <script> from a record reached the page", path)
		}
	}
}

func TestHTMLInABodyIsShownAsText(t *testing.T) {
	s, h := seeded(t)
	got := body(t, get(t, h, "/log/"+logSlug(t, s)))
	if !strings.Contains(got, "&lt;script&gt;alert(1)&lt;/script&gt;") || !strings.Contains(got, "&lt;b&gt;more&lt;/b&gt;") {
		t.Errorf("HTML in the body is not shown as text:\n%s", got)
	}
}

func TestWikilinksInBodiesResolve(t *testing.T) {
	s, h := seeded(t)
	got := body(t, get(t, h, "/log/"+logSlug(t, s)))
	if !strings.Contains(got, `<a href="/go/acme-reporting">the reporting work</a>`) {
		t.Fatalf("the wikilink is not a link:\n%s", got)
	}
	res := get(t, h, "/go/acme-reporting")
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/engagements/acme-reporting" {
		t.Errorf("/go/acme-reporting: %d to %q", res.StatusCode, res.Header.Get("Location"))
	}
	if res := get(t, h, "/go/INV-2026-001"); res.Header.Get("Location") != "/invoices/INV-2026-001" {
		t.Errorf("an invoice number went to %q", res.Header.Get("Location"))
	}
	if res := get(t, h, "/go/nobody"); res.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown name: %d", res.StatusCode)
	}
}

func TestOnlyAnswersToLocalhost(t *testing.T) {
	_, h := seeded(t)
	for host, want := range map[string]int{
		"localhost:4000":    http.StatusOK,
		"127.0.0.1:4000":    http.StatusOK,
		"[::1]:4000":        http.StatusOK,
		"evil.example:4000": http.StatusForbidden,
		"127.0.0.1.nip.io":  http.StatusForbidden,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("Host %s: %d, want %d", host, w.Code, want)
		}
	}
}

func TestNothingWrites(t *testing.T) {
	_, h := seeded(t)
	r := httptest.NewRequest(http.MethodPost, "http://localhost/clients/acme", strings.NewReader("status=cold"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", w.Code)
	}
}

func TestMissingRecordsAre404s(t *testing.T) {
	_, h := seeded(t)
	for _, path := range []string{"/clients/nobody", "/invoices/INV-1999-001", "/log/nothing", "/nope", "/invoices/INV-2026-001/pdf"} {
		if res := get(t, h, path); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, res.StatusCode)
		}
	}
}

func TestServesTheInvoicePDF(t *testing.T) {
	s, h := seeded(t)
	if err := os.MkdirAll(s.FilesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.FilesDir(), "INV-2026-001.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := get(t, h, "/invoices/INV-2026-001/pdf")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if !strings.Contains(body(t, get(t, h, "/invoices/INV-2026-001")), "/invoices/INV-2026-001/pdf") {
		t.Error("the invoice page does not link the PDF")
	}
}

func TestReadsTheFilesEachTime(t *testing.T) {
	s, h := seeded(t)
	if _, err := s.AddClient(store.NewClient{Slug: "globex", Name: "Globex"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body(t, get(t, h, "/clients/")), "Globex") {
		t.Error("a client added after the server started is missing")
	}
}

func TestItemWorkShowsWhatWasDelivered(t *testing.T) {
	s, h := seeded(t)
	if _, err := s.AddEngagement(store.NewEngagement{Client: "acme", Name: "articles", Title: "Articles", Basis: "item", Rate: "750", Unit: "article"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDelivery(store.NewDelivery{Engagement: "acme-articles", Items: "2", What: "Two tips", Date: "2026-09-12"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]string{
		"/engagements/acme-articles": {"750.00 per article", "2 articles", "Two tips"},
		"/time/?month=2026-09":       {"Delivered", "2 articles", "Two tips"},
		"/stats/?month=2026-09":      {"Delivered", "2 articles", "1,500.00"},
	} {
		got := body(t, get(t, h, path))
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %q", path, w)
			}
		}
	}
}
