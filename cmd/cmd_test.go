package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// run executes one mavis command against an isolated config, cache and
// records directory, and returns stdout.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCommand("test")
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("MAVIS_ROOT", "")
	dir := t.TempDir()
	if _, err := run(t, "init", dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInitRemembersTheRoot(t *testing.T) {
	setup(t)
	// No --root and no env: the config written by init is what finds it.
	out, err := run(t, "client", "add", "acme", "--name", "Acme Ltd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Added Acme Ltd (acme), active") {
		t.Fatalf("out = %q", out)
	}
}

func TestInitWillNotSilentlyRepoint(t *testing.T) {
	setup(t)
	if _, err := run(t, "init", t.TempDir()); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if _, err := run(t, "init", "--force", t.TempDir()); err != nil {
		t.Fatalf("--force: %v", err)
	}
}

func TestClientLifecycle(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd", "--contact", "Jo Bloggs")
	mustRun(t, "client", "add", "globex", "--status", "prospect")

	out := mustRun(t, "client", "warm", "acm")
	if out != "Acme Ltd is now warm\n" {
		t.Fatalf("move: %q", out)
	}
	if out := mustRun(t, "client", "warm", "acme"); !strings.Contains(out, "already warm") {
		t.Fatalf("repeat move: %q", out)
	}

	out = mustRun(t, "client", "list", "--status", "warm")
	if !strings.Contains(out, "acme") || strings.Contains(out, "globex") {
		t.Fatalf("filtered list:\n%s", out)
	}

	var clients []map[string]any
	if err := json.Unmarshal([]byte(mustRun(t, "client", "list", "--json")), &clients); err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("json list: %v", clients)
	}

	out = mustRun(t, "client", "show", "acme")
	if !strings.Contains(out, "Jo Bloggs") || !strings.Contains(out, "warm since") {
		t.Fatalf("show:\n%s", out)
	}
}

func TestEmptyJSONListIsAnArrayNotNull(t *testing.T) {
	setup(t)
	if out := mustRun(t, "client", "list", "--json"); strings.TrimSpace(out) != "[]" {
		t.Fatalf("out = %q", out)
	}
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(t, args...)
	if err != nil {
		t.Fatalf("mavis %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func TestEngagements(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "client", "add", "globex")

	out := mustRun(t, "engagement", "add", "acme", "reporting", "--title", "Reporting module", "--basis", "day", "--rate", "650")
	if !strings.Contains(out, "Reporting module (acme-reporting) for acme, active") {
		t.Fatalf("add: %q", out)
	}
	mustRun(t, "eng", "add", "globex", "audit", "--status", "proposed")

	out = mustRun(t, "engagement", "list", "--client", "glob")
	if !strings.Contains(out, "globex-audit") || strings.Contains(out, "acme-reporting") {
		t.Fatalf("list by client:\n%s", out)
	}

	out = mustRun(t, "client", "show", "acme")
	if !strings.Contains(out, "Engagements") || !strings.Contains(out, "day @ 650.00") {
		t.Fatalf("client show:\n%s", out)
	}

	if out := mustRun(t, "engagement", "done", "reporting"); out != "Reporting module is now done\n" {
		t.Fatalf("done: %q", out)
	}
	if out := mustRun(t, "engagement", "show", "reporting"); !strings.Contains(out, "End") {
		t.Fatalf("show after done:\n%s", out)
	}
}

func TestLogFollowUpsAndDone(t *testing.T) {
	setup(t)
	t.Setenv("EDITOR", "false") // would fail if it were ever opened
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "engagement", "add", "acme", "reporting")

	out := mustRun(t, "log", "call", "acme", "Scoped", "the", "module", "-e", "reporting",
		"-f", "Send estimate", "-f", "Share staging", "--due", "2026-01-01", "--due", "+7d")
	if !strings.Contains(out, "Logged call with acme: log/") || !strings.Contains(out, "2 follow-ups") {
		t.Fatalf("log: %q", out)
	}
	// No summary, but tests have no terminal, so no editor.
	mustRun(t, "note", "acme")

	out = mustRun(t, "follow-ups", "--overdue")
	if !strings.Contains(out, "Send estimate") || strings.Contains(out, "Share staging") || !strings.Contains(out, "2026-01-01 !") {
		t.Fatalf("overdue:\n%s", out)
	}

	out = mustRun(t, "client", "show", "acme")
	for _, want := range []string{"Follow-ups", "Recent", "Scoped the module"} {
		if !strings.Contains(out, want) {
			t.Errorf("client show missing %q:\n%s", want, out)
		}
	}

	if out := mustRun(t, "done", "acme", "estimate"); out != "Done: Send estimate (acme)\n" {
		t.Fatalf("done: %q", out)
	}
	if out := mustRun(t, "follow-ups"); strings.Contains(out, "Send estimate") || !strings.Contains(out, "Share staging") {
		t.Fatalf("after done:\n%s", out)
	}
}

func TestDueNeedsMatchingFollowUps(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme")
	if _, err := run(t, "log", "call", "acme", "x", "--due", "+1d"); err == nil {
		t.Error("--due without a follow-up should fail")
	}
	if _, err := run(t, "log", "call", "acme", "x", "-f", "a", "-f", "b", "-f", "c", "--due", "+1d", "--due", "+2d"); err == nil {
		t.Error("mismatched --due count should fail")
	}
}

func TestTodayOnAnEmptyBook(t *testing.T) {
	setup(t)
	if out := mustRun(t, "today"); out != "Nothing needs you today.\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestTodayShowsOverdueAndActiveWork(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme")
	mustRun(t, "engagement", "add", "acme", "reporting", "--title", "Reporting module", "--basis", "day")
	mustRun(t, "log", "call", "acme", "x", "-f", "Send estimate", "--due", "2020-01-01")

	out := mustRun(t, "today")
	for _, want := range []string{"Overdue", "Send estimate", "acme · due 2020-01-01", "days ago", "Active engagements", "Reporting module"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(mustRun(t, "today", "--json")), &parsed); err != nil {
		t.Fatal(err)
	}
}
