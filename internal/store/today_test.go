package store

import (
	"testing"
	"time"
)

var defaultQuiet = Quiet{ActiveQuiet: 14, WarmKeepInTouch: 30, WarmToCold: 60}

func at(s *Store, y int, m time.Month, d int) {
	s.Now = func() time.Time { return time.Date(y, m, d, 9, 0, 0, 0, time.UTC) }
}

func names(ns []Nudge) map[string]string {
	out := map[string]string{}
	for _, n := range ns {
		out[n.Client] = n.Suggest
	}
	return out
}

func TestTodayNudges(t *testing.T) {
	s := newStore(t)
	at(s, 2026, 6, 1)
	s.AddClient(NewClient{Slug: "busy"}) // active, active engagement
	s.AddClient(NewClient{Slug: "idle"}) // active, nothing on
	s.AddClient(NewClient{Slug: "lukewarm", Status: "warm"})
	s.AddClient(NewClient{Slug: "fading", Status: "warm"})
	s.AddClient(NewClient{Slug: "frozen", Status: "cold"})
	s.AddClient(NewClient{Slug: "lead", Status: "prospect"})
	s.AddEngagement(NewEngagement{Client: "busy", Name: "build"})

	at(s, 2026, 8, 20)
	s.AddLog(NewLog{Kind: "call", Client: "lukewarm"}) // 42 days before today
	at(s, 2026, 7, 1)
	s.AddLog(NewLog{Kind: "call", Client: "fading"}) // 92 days before today

	at(s, 2026, 10, 1)
	today, _, err := s.Today(defaultQuiet)
	if err != nil {
		t.Fatal(err)
	}

	moves := names(today.Moves)
	if len(moves) != 2 || moves["idle"] != "warm" || moves["fading"] != "cold" {
		t.Errorf("moves = %+v", today.Moves)
	}
	if keep := names(today.KeepInTouch); len(keep) != 1 || keep["lukewarm"] != "" {
		t.Errorf("keep in touch = %+v", today.KeepInTouch)
	}
	if len(today.Engagements) != 1 || today.Engagements[0].Client != "busy" {
		t.Errorf("engagements = %+v", today.Engagements)
	}
}

func TestMovingAClientResetsItsQuietClock(t *testing.T) {
	s := newStore(t)
	at(s, 2026, 1, 1)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddLog(NewLog{Kind: "call", Client: "acme"})

	at(s, 2026, 9, 30)
	s.SetClientStatus("acme", "warm") // a fresh judgement, yesterday

	at(s, 2026, 10, 1)
	today, _, _ := s.Today(defaultQuiet)
	if len(today.Moves)+len(today.KeepInTouch) != 0 {
		t.Fatalf("a client moved yesterday should not be nudged: %+v %+v", today.Moves, today.KeepInTouch)
	}
}

func TestTodayFollowUps(t *testing.T) {
	s := newStore(t)
	s.AddClient(NewClient{Slug: "acme"})
	s.AddLog(NewLog{Kind: "note", Client: "acme", FollowUps: []NewFollowUp{
		{Text: "late", Due: "2026-09-28"},
		{Text: "soon", Due: "2026-10-03"},
		{Text: "edge of week", Due: "2026-10-07"},
		{Text: "next week", Due: "2026-10-08"},
		{Text: "someday"},
	}})
	today, _, _ := s.Today(defaultQuiet)
	if len(today.Overdue) != 1 || today.Overdue[0].Text != "late" {
		t.Errorf("overdue = %+v", today.Overdue)
	}
	if len(today.ThisWeek) != 2 || today.ThisWeek[1].Text != "edge of week" {
		t.Errorf("this week = %+v", today.ThisWeek)
	}
}
