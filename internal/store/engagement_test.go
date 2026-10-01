package store

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestAddEngagement(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme", Name: "Acme Ltd"})

	e, err := s.AddEngagement(NewEngagement{Client: "acme", Name: "reporting", Title: "Reporting module", Basis: "day", Rate: "650", Project: "acme-reporting-app"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(e.Path)
	want := `---
type: engagement
status: active
client: '[[acme]]'
title: Reporting module
basis: day
rate: "650.00"
start: 2026-10-01
project: '[[acme-reporting-app]]'
---
`
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	if e.Slug != "acme-reporting" || e.Client != "acme" || e.Project != "acme-reporting-app" {
		t.Fatalf("parsed %+v", e)
	}
}

func TestAddEngagementValidates(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})

	for name, in := range map[string]NewEngagement{
		"unknown client": {Client: "nobody", Name: "x"},
		"bad basis":      {Client: "acme", Name: "x", Basis: "weekly"},
		"bad rate":       {Client: "acme", Name: "x", Rate: "six hundred"},
		"bad status":     {Client: "acme", Name: "x", Status: "maybe"},
		"bad name":       {Client: "acme", Name: "Big Rebuild"},
	} {
		if _, err := s.AddEngagement(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	// Nothing invoicing needs is required.
	e, err := s.AddEngagement(NewEngagement{Client: "acme", Name: "audit", Status: "proposed"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Start != "" || e.Basis != "" || e.Rate != "" {
		t.Errorf("a proposed engagement should carry nothing it was not given: %+v", e)
	}
}

func TestEngagementStatusStampsDates(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddEngagement(NewEngagement{Client: "acme", Name: "audit", Status: "proposed"})

	s.Now = func() time.Time { return time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC) }
	e, changed, err := s.SetEngagementStatus("audit", "active")
	if err != nil || !changed || e.Start != "2026-10-06" {
		t.Fatalf("activate: %+v %v %v", e, changed, err)
	}

	s.Now = func() time.Time { return time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) }
	e, _, _ = s.SetEngagementStatus("audit", "done")
	if e.End != "2026-11-20" || e.Start != "2026-10-06" {
		t.Fatalf("done: %+v", e)
	}
}

func TestLinkTargetReadsHandWrittenForms(t *testing.T) {
	for in, want := range map[string]string{
		"[[acme]]":          "acme",
		"acme":              "acme",
		"[[acme|Acme Ltd]]": "acme",
		"[[clients/acme]]":  "acme",
		"[[acme#Contacts]]": "acme",
		" [[acme]] ":        "acme",
	} {
		if got := linkTarget(in); got != want {
			t.Errorf("linkTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEngagementClientReadsAHandEditedLink(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	e, _ := s.AddEngagement(NewEngagement{Client: "acme", Name: "audit"})

	data, _ := os.ReadFile(e.Path)
	os.WriteFile(e.Path, []byte(strings.Replace(string(data), "'[[acme]]'", "\"[[acme|Acme Ltd]]\"", 1)), 0o644)

	all, _, _ := s.Engagements()
	if len(all) != 1 || all[0].Client != "acme" {
		t.Fatalf("got %+v", all)
	}
}
