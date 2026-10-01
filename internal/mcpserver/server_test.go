package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JustSteveKing/mavis/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a real client to a real server over an in-memory transport,
// so these tests go through the protocol, schema validation included.
func connect(t *testing.T) (*mcp.ClientSession, *store.Store) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Init()
	s.Actor = "agent"
	s.Now = func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) }

	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(s, Options{Quiet: store.Quiet{ActiveQuiet: 14, WarmKeepInTouch: 30, WarmToCold: 60}, Signoff: "Steve"}, "test").Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, s
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s: tool error: %s", name, text(res))
	}
	if out == nil {
		return
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("%s: decode %s: %v", name, raw, err)
	}
}

func callErr(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s should have failed", name)
	}
	return text(res)
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestTheLineIsHeld(t *testing.T) {
	cs, _ := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"get_today", "log_interaction", "log_time", "draft_invoice", "draft_quote", "draft_reminder"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s", want)
		}
	}
	// Irreversible or outward-facing: not there at all.
	for _, name := range names {
		for _, banned := range []string{"issue", "send", "paid", "accept", "decline", "discard", "delete"} {
			if strings.Contains(name, banned) {
				t.Errorf("%s should not be an agent tool", name)
			}
		}
	}
}

func TestACallBecomesTomorrowsToDo(t *testing.T) {
	cs, _ := connect(t)
	var c store.Client
	call(t, cs, "add_client", map[string]any{"slug": "acme", "name": "Acme Ltd", "contact": "Jo Bloggs"}, &c)

	var l store.LogEntry
	call(t, cs, "log_interaction", map[string]any{
		"kind": "call", "client": "acme", "summary": "Scoped the reporting module.",
		"follow_ups": []map[string]any{{"text": "Send estimate", "due": "2026-09-30"}},
	}, &l)
	data, _ := os.ReadFile(l.Path)
	if !strings.Contains(string(data), "by: agent") {
		t.Errorf("an agent's entry should say so:\n%s", data)
	}

	var today todayOut
	call(t, cs, "get_today", map[string]any{}, &today)
	if len(today.Overdue) != 1 || today.Overdue[0].Text != "Send estimate" {
		t.Fatalf("today: %+v", today.Overdue)
	}

	var f store.FollowUp
	call(t, cs, "complete_follow_up", map[string]any{"log": today.Overdue[0].Log, "text": "Send estimate"}, &f)
	if !f.Done {
		t.Fatal("not done")
	}
	if msg := callErr(t, cs, "complete_follow_up", map[string]any{"log": today.Overdue[0].Log, "text": "Send"}); !strings.Contains(msg, "no follow-up") {
		t.Errorf("a partial text should not match: %s", msg)
	}
}

func TestExactIdentifiersOnly(t *testing.T) {
	cs, _ := connect(t)
	call(t, cs, "add_client", map[string]any{"slug": "acme"}, nil)
	msg := callErr(t, cs, "log_interaction", map[string]any{"kind": "note", "client": "acm", "summary": "x"})
	if !strings.Contains(msg, `no client with slug "acm"`) || !strings.Contains(msg, "list_clients") {
		t.Fatalf("got %s", msg)
	}
}

func TestTimeToDraftInvoice(t *testing.T) {
	cs, _ := connect(t)
	call(t, cs, "add_client", map[string]any{"slug": "acme", "name": "Acme Ltd"}, nil)
	call(t, cs, "add_engagement", map[string]any{"client": "acme", "name": "reporting", "title": "Reporting", "basis": "day", "rate": "650"}, nil)
	call(t, cs, "log_time", map[string]any{"engagement": "acme-reporting", "duration": "2d", "date": "2026-09-10", "what": "Filters"}, nil)

	var d draftInvoiceOut
	call(t, cs, "draft_invoice", map[string]any{"client": "acme", "month": "2026-09"}, &d)
	if d.Draft.Status != "draft" || d.Draft.Net != 130000 || d.Next != "mavis invoice issue draft-acme-2026-09" {
		t.Fatalf("draft: %+v", d)
	}

	// More time into an invoiced month warns rather than failing.
	var logged timeLogged
	call(t, cs, "log_time", map[string]any{"engagement": "acme-reporting", "duration": "1d", "date": "2026-09-20"}, &logged)
	if !strings.Contains(logged.Warning, "already on draft-acme-2026-09") {
		t.Fatalf("warning: %q", logged.Warning)
	}

	var stats store.Stats
	call(t, cs, "get_stats", map[string]any{"month": "2026-09"}, &stats)
	if len(stats.Engagements) != 1 || stats.Engagements[0].Minutes != 3*450 {
		t.Fatalf("stats: %+v", stats.Engagements)
	}
}

func TestDraftReminderLogsNothing(t *testing.T) {
	cs, s := connect(t)
	s.Actor = "" // set up as the human would
	s.AddClient(store.NewClient{Slug: "acme", Name: "Acme Ltd", Contact: "Jo Bloggs"})
	s.SetClient("acme", store.ClientUpdate{Address: []string{"1 High Street"}})
	d, _, _ := s.AddInvoice(store.NewInvoice{Client: "acme", Lines: []store.ManualLine{{Description: "Work", Price: "1000"}}})
	inv, err := s.IssueInvoice(d.Slug, store.IssueOptions{Issuer: store.Issuer{Name: "Steve", Address: []string{"1 My St"}, VATNumber: "GB999999973"}, Date: "2026-08-01"})
	if err != nil {
		t.Fatal(err)
	}

	var today todayOut
	call(t, cs, "get_today", map[string]any{}, &today)
	if len(today.Chase) != 1 || !today.Chase[0].NextDue || today.Chase[0].NextReminder != 1 {
		t.Fatalf("chase: %+v", today.Chase)
	}

	var r reminderOut
	call(t, cs, "draft_reminder", map[string]any{"ref": inv.Number}, &r)
	if r.Stage != 1 || !strings.HasPrefix(r.Body, "Hi Jo,") || !strings.Contains(r.Next, "--sent") {
		t.Fatalf("reminder: %+v", r)
	}
	if sent, _ := s.Reminders(inv.Number); len(sent) != 0 {
		t.Fatal("drafting a reminder must not record it as sent")
	}
}
