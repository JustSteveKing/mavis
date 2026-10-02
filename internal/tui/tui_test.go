package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JustSteveKing/mavis/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func seeded(t *testing.T) (*store.Store, model) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Init()
	s.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }
	s.AddClient(store.NewClient{Slug: "acme", Name: "Acme Ltd", Contact: "Jo Bloggs"})
	s.SetClient("acme", store.ClientUpdate{Address: []string{"1 High Street"}})
	s.AddLog(store.NewLog{Kind: "call", Client: "acme", Summary: "Scoped it.", FollowUps: []store.NewFollowUp{{Text: "Send estimate", Due: "2026-09-28"}}})
	d, _, _ := s.AddInvoice(store.NewInvoice{Client: "acme", Lines: []store.ManualLine{{Description: "Workshop", Price: "1000"}}})
	if _, err := s.IssueInvoice(d.Slug, store.IssueOptions{Issuer: store.Issuer{Name: "Steve", Address: []string{"1 My St"}, VATNumber: "GB999999973"}, Date: "2026-08-01"}); err != nil {
		t.Fatal(err)
	}
	m := New(s, Options{Quiet: store.Quiet{ActiveQuiet: 14, WarmKeepInTouch: 30, WarmToCold: 60}, Signoff: "Steve"}).(model)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return s, next.(model)
}

func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func TestCursorStartsOnARowNotAHeader(t *testing.T) {
	_, m := seeded(t)
	r := m.selected()
	if r == nil || r.invoice != "INV-2026-001" {
		t.Fatalf("selected %+v", r)
	}
	m = press(t, m, "j")
	if r := m.selected(); r == nil || r.followUp == nil {
		t.Fatalf("j should step over the section header to the follow-up: %+v", r)
	}
	m = press(t, m, "k", "k", "k")
	if r := m.selected(); r == nil || r.invoice == "" {
		t.Fatal("k past the top should stay on the first row")
	}
}

func TestTickOffAFollowUp(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "j", "x")
	if !strings.HasPrefix(m.status, "Done: Send estimate") {
		t.Fatalf("status %q", m.status)
	}
	if open, _, _ := s.FollowUps(); len(open) != 0 {
		t.Fatal("the follow-up should be ticked in the file")
	}
	for _, r := range m.rows {
		if r.followUp != nil {
			t.Fatal("and gone from Today")
		}
	}
}

func TestAddANote(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "n")
	if m.mode != modeNote {
		t.Fatal("n should open the note input")
	}
	for _, r := range "Called back about the PO" {
		m = press(t, m, string(r))
	}
	m = press(t, m, "enter")
	logs, _, _ := s.Logs()
	last := logs[len(logs)-1]
	if last.Kind != "note" || last.Summary != "Called back about the PO" || last.Client != "acme" {
		t.Fatalf("logged %+v", last)
	}
	data, _ := os.ReadFile(last.Path)
	if strings.Contains(string(data), "by: agent") {
		t.Error("a note typed in the TUI is the human's")
	}

	m = press(t, m, "n", "a", "esc")
	if m.mode != modeNormal || m.status != "Note dropped" {
		t.Fatalf("esc should drop the note: %v %q", m.mode, m.status)
	}
}

func TestMarkPaidAsksFirst(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "p")
	if m.mode != modeConfirmPaid || !strings.Contains(m.statusLine(), "Mark INV-2026-001 paid today") {
		t.Fatalf("should ask: %q", m.statusLine())
	}
	m = press(t, m, "n")
	if inv, _ := s.ResolveInvoice("INV-2026-001"); inv.Status != "issued" {
		t.Fatal("n must leave it unpaid")
	}
	m = press(t, m, "p", "y")
	if inv, _ := s.ResolveInvoice("INV-2026-001"); inv.Status != "paid" || inv.Paid != "2026-10-01" {
		t.Fatalf("y should mark it paid: %+v", inv)
	}
	if !strings.Contains(m.status, "paid on 2026-10-01") {
		t.Fatalf("status %q", m.status)
	}
}

func TestReminderPreviewLogsNothing(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "r")
	if !strings.Contains(m.detailNote, "Subject: Invoice INV-2026-001: a reminder") || !strings.Contains(m.detailNote, "Hi Jo,") {
		t.Fatalf("preview:\n%s", m.detailNote)
	}
	if sent, _ := s.Reminders("INV-2026-001"); len(sent) != 0 {
		t.Fatal("a preview must not be recorded as sent")
	}
	if m = press(t, m, "j"); m.detailNote != "" {
		t.Fatal("moving on should put the detail back")
	}
}

func TestEnterOpensTheInvoice(t *testing.T) {
	_, m := seeded(t)
	m = press(t, m, "enter")
	if m.view != viewInvoices {
		t.Fatalf("view %v", m.view)
	}
	if r := m.selected(); r == nil || r.invoice != "INV-2026-001" {
		t.Fatalf("selected %+v", r)
	}
}

func TestAgentChangesAppearOnTheTickButNotWhileTyping(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "tab", "j", "tab") // to Clients
	s.Actor = "agent"
	s.AddClient(store.NewClient{Slug: "globex", Name: "Globex"})

	m = press(t, m, "n") // typing a note
	next, _ := m.Update(tickMsg(time.Now()))
	m = next.(model)
	if strings.Contains(rowsText(m), "globex") {
		t.Fatal("must not reload under someone typing")
	}
	m = press(t, m, "esc")
	next, _ = m.Update(tickMsg(time.Now()))
	m = next.(model)
	if !strings.Contains(rowsText(m), "globex") {
		t.Fatal("the tick should pick up the new client")
	}
}

func TestFrameFitsTheTerminal(t *testing.T) {
	_, m := seeded(t)
	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 15}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		frame := next.(model).View()
		lines := strings.Split(frame, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
	}
}

func TestFooterOffersOnlyWhatApplies(t *testing.T) {
	_, m := seeded(t)
	if f := m.footer(); !strings.Contains(f, "paid") || !strings.Contains(f, "reminder") || strings.Contains(f, "done") {
		t.Fatalf("on an overdue invoice: %s", f)
	}
	m = press(t, m, "j")
	if f := m.footer(); strings.Contains(f, "paid") || !strings.Contains(f, "done") {
		t.Fatalf("on a follow-up: %s", f)
	}
}

func rowsText(m model) string {
	var b strings.Builder
	for _, r := range m.rows {
		b.WriteString(r.text + "\n")
	}
	return b.String()
}

func typeText(t *testing.T, m model, s string) model {
	t.Helper()
	for _, r := range s {
		m = press(t, m, string(r))
	}
	return m
}

func ctrlS(t *testing.T, m model) model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	return next.(model)
}

func withEngagement(t *testing.T) (*store.Store, model) {
	t.Helper()
	s, m := seeded(t)
	s.AddEngagement(store.NewEngagement{Client: "acme", Name: "reporting", Title: "Reporting", Basis: "day", Rate: "650"})
	m.reload()
	return s, m
}

func TestLogTimeFromAForm(t *testing.T) {
	s, m := withEngagement(t)
	m = press(t, m, "t")
	if m.mode != modeForm || m.form.fields[0].value() != "acme-reporting" {
		t.Fatalf("form should open on acme's engagement: %v", m.mode)
	}

	// A bad duration keeps the form open, says why, and keeps what was typed.
	m = typeText(t, m, "ages")
	m = ctrlS(t, m)
	if m.mode != modeForm || !strings.Contains(m.form.err, "ages") {
		t.Fatalf("a bad duration should stay in the form: mode %v, err %q", m.mode, m.form.err)
	}
	if m.form.fields[1].value() != "ages" {
		t.Fatal("what was typed should be kept")
	}

	for range 4 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = next.(model)
	}
	m = typeText(t, m, "1h30m")
	m = ctrlS(t, m)
	if m.mode != modeNormal || !strings.HasPrefix(m.status, "Logged 1h30m to acme-reporting") {
		t.Fatalf("mode %v, status %q", m.mode, m.status)
	}
	if entries, _, _ := s.TimeEntries(); len(entries) != 1 || entries[0].Minutes != 90 {
		t.Fatalf("entries %+v", entries)
	}
	if m.monthMinutes != 90 {
		t.Fatalf("the summary should count it: %d", m.monthMinutes)
	}
}

func TestLogTimeNeedsAnEngagement(t *testing.T) {
	_, m := seeded(t)
	if m = press(t, m, "t"); m.mode != modeNormal || !strings.Contains(m.status, "no engagement") {
		t.Fatalf("mode %v, status %q", m.mode, m.status)
	}
}

func TestLogACallWithAFollowUp(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "l")
	m = typeText(t, m, "Agreed the PO")
	m = press(t, m, "tab")
	m = typeText(t, m, "Send the contract")
	m = press(t, m, "tab")
	m = typeText(t, m, "+2d")
	m = press(t, m, "enter") // enter on the last field saves
	if m.mode != modeNormal || !strings.Contains(m.status, "follow-up: Send the contract") {
		t.Fatalf("mode %v, status %q", m.mode, m.status)
	}
	logs, _, _ := s.Logs()
	last := logs[len(logs)-1]
	if last.Kind != "call" || len(last.FollowUps) != 1 || last.FollowUps[0].Due != "2026-10-03" {
		t.Fatalf("logged %+v", last)
	}

	// A due date without a follow-up is refused, in the form.
	m = press(t, m, "l")
	m = typeText(t, m, "Quick chat")
	m = press(t, m, "tab", "tab")
	m = typeText(t, m, "+1d")
	m = ctrlS(t, m)
	if m.mode != modeForm || !strings.Contains(m.form.err, "needs a follow-up") {
		t.Fatalf("mode %v err %q", m.mode, m.form.err)
	}
	if m = press(t, m, "esc"); m.mode != modeNormal || m.status != "Cancelled" {
		t.Fatalf("esc: %v %q", m.mode, m.status)
	}
}

func TestMoveAClient(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "m")
	if m.mode != modeMove || !strings.Contains(m.statusLine(), "Move acme to:") {
		t.Fatalf("prompt: %q", m.statusLine())
	}
	m = press(t, m, "w")
	if c, _ := s.ResolveClient("acme"); c.Status != "warm" {
		t.Fatalf("status %s", c.Status)
	}
	if m = press(t, m, "m", "x"); m.status != "Not moved" {
		t.Fatalf("other keys cancel: %q", m.status)
	}
}

func TestRel(t *testing.T) {
	for date, want := range map[string]string{
		"2026-10-01": "today", "2026-10-02": "tomorrow", "2026-09-30": "yesterday",
		"2026-10-05": "in 4 days", "2026-09-19": "12 days ago",
		"2026-03-03": "on 3 Mar", "2025-12-25": "on 25 Dec 2025",
	} {
		if got := rel(date, "2026-10-01"); got != want {
			t.Errorf("rel(%s) = %q, want %q", date, got, want)
		}
	}
}

func TestColumnsAlign(t *testing.T) {
	rows := []row{item("", "a", "bb", "c"), item("", "aaaa", "b", "c")}
	w := columnWidths(rows)
	if got := alignCells(rows[0].cells, w); got != "a     bb  c" {
		t.Fatalf("%q", got)
	}
	long := strings.Repeat("x", 50)
	if got := alignCells([]string{long, "y"}, columnWidths([]row{item("", long, "y")})); lipgloss.Width(got) != maxCell+2+1 {
		t.Fatalf("a long cell should be capped: %q", got)
	}
}
