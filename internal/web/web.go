// Package web serves the records as a small read-only site on localhost.
//
// Every request reads the files again, so the page you load is the folder
// as it is now: a call logged at the CLI, by an agent over MCP or in
// Obsidian shows up on refresh. Nothing here writes. Changing a record stays
// with the CLI, the TUI and your editor, where the guards already are.
//
// Records are private, so the handler answers only requests addressed to
// this machine by name (see local), which is what stops another site in the
// same browser reading them through DNS rebinding.
package web

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/JustSteveKing/mavis/internal/money"
	"github.com/JustSteveKing/mavis/internal/pdf"
	"github.com/JustSteveKing/mavis/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/style.css
var styleCSS []byte

// Options carries what the pages need from the config.
type Options struct {
	Quiet store.Quiet

	// ErrorLog receives errors from reading the records; nil discards them.
	ErrorLog *log.Logger
}

type server struct {
	s     *store.Store
	o     Options
	pages map[string]*template.Template
}

// New returns the site's handler.
func New(s *store.Store, o Options) (http.Handler, error) {
	srv := &server{s: s, o: o, pages: map[string]*template.Template{}}
	if srv.o.ErrorLog == nil {
		srv.o.ErrorLog = log.New(discard{}, "", 0)
	}
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		base := strings.TrimSuffix(filepath.Base(name), ".html")
		if base == "layout" {
			continue
		}
		t, err := template.New("layout.html").Funcs(srv.funcs()).ParseFS(templateFS, "templates/layout.html", name)
		if err != nil {
			return nil, err
		}
		srv.pages[base] = t
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", srv.todayPage)
	mux.HandleFunc("GET /clients/{$}", srv.clients)
	mux.HandleFunc("GET /clients/{slug}", srv.client)
	mux.HandleFunc("GET /engagements/{$}", srv.engagements)
	mux.HandleFunc("GET /engagements/{slug}", srv.engagement)
	mux.HandleFunc("GET /log/{$}", srv.logs)
	mux.HandleFunc("GET /log/{slug}", srv.logEntry)
	mux.HandleFunc("GET /follow-ups/{$}", srv.followUps)
	mux.HandleFunc("GET /time/{$}", srv.timePage)
	mux.HandleFunc("GET /invoices/{$}", srv.invoices)
	mux.HandleFunc("GET /invoices/{slug}", srv.invoice)
	mux.HandleFunc("GET /invoices/{slug}/pdf", srv.invoicePDF)
	mux.HandleFunc("GET /quotes/{$}", srv.quotes)
	mux.HandleFunc("GET /quotes/{slug}", srv.quote)
	mux.HandleFunc("GET /quotes/{slug}/pdf", srv.quotePDF)
	mux.HandleFunc("GET /stats/{$}", srv.stats)
	mux.HandleFunc("GET /go/{name}", srv.follow)
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(styleCSS)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		srv.notFound(w, r, "There is no page at "+r.URL.Path+".")
	})
	return local(mux), nil
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// local refuses any request not addressed to this machine by name, and
// keeps pages out of frames and away from scripts.
//
// A page on another site can point a hostname it controls at 127.0.0.1 and
// then read from it as its own origin; the request still arrives carrying
// that hostname in Host. Answering only to localhost names closes that.
func local(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		host = strings.Trim(host, "[]")
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "mavis web only answers to localhost", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csp is set on pages, not on PDFs, whose viewer it would block.
const csp = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// page is what every template gets.
type page struct {
	Title    string
	Section  string // the nav item to mark current
	Root     string
	Today    string
	Problems []store.Problem
	Data     any
}

func (srv *server) render(w http.ResponseWriter, status int, name string, p page) {
	p.Root = srv.s.Root()
	p.Today = srv.date()
	var buf bytes.Buffer
	if err := srv.pages[name].Execute(&buf, p); err != nil {
		srv.o.ErrorLog.Printf("%s: %v", name, err)
		http.Error(w, "could not render this page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (srv *server) fail(w http.ResponseWriter, err error) {
	srv.o.ErrorLog.Print(err)
	srv.render(w, http.StatusInternalServerError, "error", page{Title: "Could not read the records", Data: err.Error()})
}

func (srv *server) notFound(w http.ResponseWriter, r *http.Request, msg string) {
	srv.render(w, http.StatusNotFound, "error", page{Title: "Not found", Data: msg})
}

func (srv *server) date() string { return srv.s.Now().Format("2006-01-02") }

// servePDF sends a generated PDF by its file name in the files folder. The
// name comes from the record, never from the request.
func (srv *server) servePDF(w http.ResponseWriter, r *http.Request, name string) {
	path := filepath.Join(srv.s.FilesDir(), name)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		srv.notFound(w, r, name+" has not been written yet.")
		return
	}
	if err != nil {
		srv.fail(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		srv.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (srv *server) pdfExists(number, slug string) bool {
	_, err := os.Stat(filepath.Join(srv.s.FilesDir(), pdf.Name(number, slug)))
	return err == nil
}

// amount is money with its currency, the way the CLI writes it.
func amount(p money.Pence, currency string) string {
	if currency == "" {
		return p.Display()
	}
	return p.Display() + " " + currency
}
