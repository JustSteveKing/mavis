package tui

import (
	"os"
	"reflect"
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

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// drive does what Bubble Tea's runtime would: it runs the commands an update
// returns and feeds their messages back in. huh moves between fields with
// messages of its own, so a form only advances like this. Commands that do
// not return quickly (a cursor blink, the reload tick) are dropped, so no
// test waits on a timer.
func drive(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("too many messages: a loop?")
		}
		msg, queue = queue[0], queue[1:]
		next, cmd := m.Update(msg)
		m = next.(model)
		queue = append(queue, runCmd(cmd)...)
	}
	return m
}

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		if msg == nil {
			return nil
		}
		// Batches and sequences are slices of commands, whatever their type.
		v := reflect.ValueOf(msg)
		if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
			var out []tea.Msg
			for i := 0; i < v.Len(); i++ {
				out = append(out, runCmd(v.Index(i).Interface().(tea.Cmd))...)
			}
			return out
		}
		switch msg.(type) {
		case tickMsg, clearStatusMsg:
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(30 * time.Millisecond):
		return nil
	}
}

func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		m = drive(t, m, keyMsg(k))
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
	if m.mode != modeForm || m.formTitle != "Note" {
		t.Fatal("n should open the note form")
	}
	m = typeText(t, m, "Called back about the PO")
	m = press(t, m, "enter")
	logs, _, _ := s.Logs()
	last := logs[len(logs)-1]
	if m.mode != modeNormal || last.Kind != "note" || last.Summary != "Called back about the PO" || last.Client != "acme" {
		t.Fatalf("mode %v, logged %+v", m.mode, last)
	}
	data, _ := os.ReadFile(last.Path)
	if strings.Contains(string(data), "by: agent") {
		t.Error("a note typed in the TUI is the human's")
	}

	m = press(t, m, "n", "a", "esc")
	if m.mode != modeNormal || m.status != "Cancelled" {
		t.Fatalf("esc should cancel: %v %q", m.mode, m.status)
	}
	// An empty note is refused in the form, not logged.
	m = press(t, m, "n", "enter")
	if m.mode != modeForm || !strings.Contains(m.View(), "A note is needed") {
		t.Fatalf("an empty note should stay in the form:\n%s", m.View())
	}
}

func TestMarkPaidAsksFirst(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "p")
	if m.mode != modeForm || !strings.Contains(m.View(), "Mark INV-2026-001 paid today") {
		t.Fatalf("should ask:\n%s", m.View())
	}
	m = press(t, m, "n")
	if inv, _ := s.ResolveInvoice("INV-2026-001"); inv.Status != "issued" || m.status != "Left unpaid" {
		t.Fatalf("n must leave it unpaid: %s %q", inv.Status, m.status)
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

	m = press(t, m, "n") // a form is open
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
	if m.mode != modeForm || m.formTitle != "Log time for acme" {
		t.Fatalf("form should open: %v %q", m.mode, m.formTitle)
	}
	m = press(t, m, "enter") // the engagement is already acme-reporting

	// A duration mavis cannot read is refused in place, input kept.
	m = typeText(t, m, "ages")
	m = press(t, m, "enter")
	if m.mode != modeForm || !strings.Contains(m.View(), "ages") || !strings.Contains(m.View(), "use 1d") {
		t.Fatalf("a bad duration should stay in the form:\n%s", m.View())
	}
	m = press(t, m, "backspace", "backspace", "backspace", "backspace")
	m = typeText(t, m, "1h30m")
	m = press(t, m, "enter", "enter", "enter") // duration, what, date
	if m.mode != modeNormal || !strings.HasPrefix(m.status, "Logged 1h30m to acme-reporting") {
		t.Fatalf("mode %v, status %q\n%s", m.mode, m.status, m.View())
	}
	if entries, _, _ := s.TimeEntries(); len(entries) != 1 || entries[0].Minutes != 90 {
		t.Fatalf("entries %+v", entries)
	}
	if m.monthMinutes != 90 {
		t.Fatalf("the summary should count it: %d", m.monthMinutes)
	}
}

func TestRecordADeliveryFromAForm(t *testing.T) {
	s, m := withEngagement(t)
	if m = press(t, m, "d"); m.mode != modeNormal || !strings.Contains(m.status, "no item work") {
		t.Fatalf("day work alone should not open the form: %v %q", m.mode, m.status)
	}
	s.AddEngagement(store.NewEngagement{Client: "acme", Name: "articles", Title: "Articles", Basis: "item", Rate: "750", Unit: "article"})
	m.reload()
	m = press(t, m, "d")
	if m.mode != modeForm || m.formTitle != "Record a delivery for acme" {
		t.Fatalf("form should open: %v %q", m.mode, m.formTitle)
	}
	m = press(t, m, "enter") // the only item engagement
	m = typeText(t, m, "Queues deep dive")
	m = press(t, m, "enter", "backspace")
	m = typeText(t, m, "2")
	m = press(t, m, "enter", "enter") // items, date
	if m.mode != modeNormal || !strings.HasPrefix(m.status, "Recorded 2 articles to acme-articles") {
		t.Fatalf("mode %v, status %q\n%s", m.mode, m.status, m.View())
	}
	if entries, _, _ := s.TimeEntries(); len(entries) != 1 || entries[0].Items != "2" || entries[0].What != "Queues deep dive" {
		t.Fatalf("entries %+v", entries)
	}
}

func TestNoInvoiceFormForAClientInvoicedElsewhere(t *testing.T) {
	s, m := seeded(t)
	where := "FreeAgent"
	s.SetClient("acme", store.ClientUpdate{Invoicing: &where})
	m.reload()
	if m = press(t, m, "i"); m.mode != modeNormal || !strings.Contains(m.status, "invoiced in FreeAgent") {
		t.Fatalf("mode %v, status %q", m.mode, m.status)
	}
	if strings.Contains(m.View(), "i invoice") {
		t.Error("the footer still offers i")
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
	m = press(t, m, "l", "enter") // kind: call
	m = typeText(t, m, "Agreed the PO")
	m = press(t, m, "enter")
	m = typeText(t, m, "Send the contract")
	m = press(t, m, "enter")
	m = typeText(t, m, "+2d")
	m = press(t, m, "enter")
	if m.mode != modeNormal || !strings.Contains(m.status, "follow-up: Send the contract") {
		t.Fatalf("mode %v, status %q\n%s", m.mode, m.status, m.View())
	}
	logs, _, _ := s.Logs()
	last := logs[len(logs)-1]
	if last.Kind != "call" || len(last.FollowUps) != 1 || last.FollowUps[0].Due != "2026-10-03" {
		t.Fatalf("logged %+v", last)
	}

	// A due date without a follow-up is refused in place.
	m = press(t, m, "l", "enter")
	m = typeText(t, m, "Quick chat")
	m = press(t, m, "enter", "enter")
	m = typeText(t, m, "+1d")
	m = press(t, m, "enter")
	if m.mode != modeForm || !strings.Contains(m.View(), "needs a follow-up") {
		t.Fatalf("should stay in the form:\n%s", m.View())
	}
	if m = press(t, m, "esc"); m.mode != modeNormal || m.status != "Cancelled" {
		t.Fatalf("esc: %v %q", m.mode, m.status)
	}
}

func TestMoveAClient(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "m")
	if m.mode != modeForm || !strings.Contains(m.View(), "Move acme to") {
		t.Fatalf("picker:\n%s", m.View())
	}
	m = press(t, m, "down", "enter") // active, then warm
	if c, _ := s.ResolveClient("acme"); c.Status != "warm" || m.status != "Acme Ltd is now warm" {
		t.Fatalf("status %s, %q", c.Status, m.status)
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

func TestNewClientFromTheTUI(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "c")
	if m.mode != modeForm || m.formTitle != "New client" {
		t.Fatalf("c should open the new client form: %v %q", m.mode, m.formTitle)
	}
	m = typeText(t, m, "Globex Corporation")
	m = press(t, m, "enter")                               // to Slug, left blank
	if !strings.Contains(m.View(), "globex-corporation") { // the suggestion shows
		t.Fatalf("the slug should be suggested from the name:\n%s", m.View())
	}
	m = press(t, m, "enter", "enter") // slug, status
	m = typeText(t, m, "Hank Scorpio")
	m = press(t, m, "enter", "enter", "enter") // contact, email, phone
	if m.mode != modeNormal || m.status != "Added Globex Corporation (globex-corporation)" {
		t.Fatalf("mode %v, status %q\n%s", m.mode, m.status, m.View())
	}
	c, err := s.ResolveClient("globex-corporation")
	if err != nil || c.Contact != "Hank Scorpio" || c.Status != "active" {
		t.Fatalf("client %+v %v", c, err)
	}
	if m.view != viewClients || m.selected().client != "globex-corporation" {
		t.Fatal("the cursor should land on the new client")
	}

	// A slug that is taken is refused as it is typed.
	m = press(t, m, "c")
	m = typeText(t, m, "Acme")
	m = press(t, m, "enter")
	m = typeText(t, m, "acme")
	m = press(t, m, "enter")
	if m.mode != modeForm || !strings.Contains(m.statusLine(), "already a client called acme") {
		t.Fatalf("should refuse a taken slug: %q", m.statusLine())
	}
}

func TestNewEngagementFromTheTUI(t *testing.T) {
	s, m := seeded(t)
	m = press(t, m, "e")
	m = typeText(t, m, "Reporting rebuild")
	m = press(t, m, "enter", "enter", "enter", "enter") // title, name, status, basis (day)
	m = typeText(t, m, "650")
	m = press(t, m, "enter", "enter", "enter") // rate, budget, start
	if m.mode != modeNormal || m.status != "Added Reporting rebuild (acme-reporting-rebuild)" {
		t.Fatalf("mode %v, status %q\n%s", m.mode, m.status, m.View())
	}
	e, err := s.ResolveEngagement("acme-reporting-rebuild")
	if err != nil || e.Basis != "day" || e.Rate != "650.00" || e.Start != "2026-10-01" {
		t.Fatalf("engagement %+v %v", e, err)
	}
}

func TestDraftAnInvoiceFromTheTUI(t *testing.T) {
	s, m := withEngagement(t)
	s.AddTime(store.NewTime{Engagement: "acme-reporting", Duration: "2d", Date: "2026-09-10"})
	m.reload()
	m = press(t, m, "i")
	if !strings.Contains(m.View(), "2026-09") {
		t.Fatalf("last month should be the default:\n%s", m.View())
	}
	m = press(t, m, "enter")
	if m.mode != modeNormal || !strings.HasPrefix(m.status, "Drafted draft-acme-2026-09: £1,560.00") {
		t.Fatalf("status %q", m.status)
	}
	if m.view != viewInvoices || m.selected().invoice != "draft-acme-2026-09" {
		t.Fatal("the cursor should land on the draft")
	}
}
