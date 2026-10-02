package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
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

func TestTime(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme")
	mustRun(t, "engagement", "add", "acme", "reporting", "--basis", "day", "--rate", "650")

	if out := mustRun(t, "time", "reporting", "1d", "Report", "filters", "--date", "2026-10-06"); out != "Logged 1d on 2026-10-06 to acme-reporting\n" {
		t.Fatalf("log: %q", out)
	}
	mustRun(t, "time", "reporting", "1h30m", "--date", "2026-10-07")
	mustRun(t, "time", "reporting", "2h", "--date", "2026-09-30")

	out := mustRun(t, "time", "list", "--month", "2026-10")
	if !strings.Contains(out, "Report filters") || strings.Contains(out, "2026-09-30") || !regexp.MustCompile(`Total\s+1\.2d\s+9h`).MatchString(out) {
		t.Fatalf("list:\n%s", out)
	}
	if out := mustRun(t, "time", "list", "--month", "all", "--client", "acme"); !strings.Contains(out, "2026-09-30") {
		t.Fatalf("all months:\n%s", out)
	}
	// Not a terminal, so edit prints the path instead of opening an editor.
	if out := mustRun(t, "time", "edit", "reporting", "--month", "2026-10"); !strings.HasSuffix(strings.TrimSpace(out), "time/acme-reporting-2026-10.md") {
		t.Fatalf("edit: %q", out)
	}
	if _, err := run(t, "time", "reporting", "ages"); err == nil {
		t.Fatal("a bad duration should fail")
	}
}

func TestStats(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme")
	mustRun(t, "client", "add", "initech")
	mustRun(t, "engagement", "add", "acme", "reporting", "--title", "Reporting", "--basis", "day", "--rate", "650")
	mustRun(t, "engagement", "add", "initech", "rebuild", "--title", "Rebuild", "--basis", "fixed", "--budget", "12000")
	mustRun(t, "time", "reporting", "2d", "--date", "2026-10-06")
	mustRun(t, "time", "rebuild", "3d", "--date", "2026-10-07")

	out := mustRun(t, "stats", "--month", "2026-10")
	for _, want := range []string{"October 2026", "Reporting", "1,300.00", "Fixed price, to date", "budget 12,000.00", "3d logged", "4,000.00 a day"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if out := mustRun(t, "stats", "--month", "2026-08"); !strings.Contains(out, "August 2026") {
		t.Errorf("empty month:\n%s", out)
	}
	if _, err := run(t, "stats", "--month", "2026-10", "--year", "2026"); err == nil {
		t.Error("two periods at once should fail")
	}
	if _, err := run(t, "stats", "--from", "2026-10-10", "--to", "2026-10-01"); err == nil {
		t.Error("a backwards range should fail")
	}
}

func TestInvoiceDrafts(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street", "--address", "Manchester M1 1AA", "--country", "gb")
	mustRun(t, "engagement", "add", "acme", "reporting", "--title", "Reporting", "--basis", "day", "--rate", "650")
	mustRun(t, "time", "reporting", "2d", "--date", "2026-10-06")

	out := mustRun(t, "invoice", "new", "acme", "--month", "2026-10", "--line", "Workshop=2 x 500 day")
	for _, want := range []string{"Drafted draft-acme-2026-10", "Reporting, October 2026", "1,300.00", "Workshop", "1,000.00", "VAT 20%", "460.00", "2,760.00 GBP"} {
		if !strings.Contains(out, want) {
			t.Errorf("new: missing %q:\n%s", want, out)
		}
	}
	if out := mustRun(t, "invoice", "list"); !strings.Contains(out, "draft-acme-2026-10") || !strings.Contains(out, "draft") {
		t.Errorf("list:\n%s", out)
	}
	if out := mustRun(t, "invoice", "show", "acme-2026"); !strings.Contains(out, "October 2026 · GBP · VAT standard") {
		t.Errorf("show:\n%s", out)
	}
	if out := mustRun(t, "client", "show", "acme"); !strings.Contains(out, "1 High Street, Manchester M1 1AA") || !strings.Contains(out, "GB") {
		t.Errorf("client show:\n%s", out)
	}

	// Time logged into a month already on an invoice draws a warning.
	root := NewRootCommand("test")
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"time", "reporting", "1d", "--date", "2026-10-20"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "already on draft-acme-2026-10") {
		t.Errorf("no warning: %q", stderr.String())
	}

	if out := mustRun(t, "invoice", "discard", "acme-2026-10"); out != "Discarded draft-acme-2026-10\n" {
		t.Errorf("discard: %q", out)
	}
}

func TestParseLineFlag(t *testing.T) {
	for in, want := range map[string]string{
		"Milestone=4000":          "Milestone|||4000",
		"Workshop=2 x 500 day":    "Workshop|2|day|500",
		"Train fare = 84.50":      "Train fare|||84.50",
		"Licence=12 x 9.99 seats": "Licence|12|seats|9.99",
	} {
		ml, err := parseLineFlag(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := ml.Description + "|" + ml.Qty + "|" + ml.Unit + "|" + ml.Price; got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"no equals", "=400", "Thing="} {
		if _, err := parseLineFlag(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestIssueAndPay(t *testing.T) {
	dir := setup(t)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "invoice", "new", "acme", "--line", "Workshop=1000")

	_, err := run(t, "invoice", "issue", "acme")
	if err == nil || !strings.Contains(err.Error(), "business.name in the config") || !strings.Contains(err.Error(), "mavis client set acme --address") {
		t.Fatalf("issue without details: %v", err)
	}

	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	data, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(data, []byte("business:\n  name: Steve Ltd\n  address: [1 My Street, Leeds]\n  vat_number: GB999999973\n")...), 0o644)
	mustRun(t, "client", "set", "acme", "--address", "1 High Street")

	out := mustRun(t, "invoice", "issue", "acme", "--date", "2026-01-05")
	if !strings.HasPrefix(out, "Issued INV-2026-001 to Acme Ltd: 1,200.00 GBP, due 2026-02-04\n") || !strings.HasSuffix(out, ".invoices/INV-2026-001.pdf\n") {
		t.Fatalf("issue: %q", out)
	}
	for _, f := range []string{"/invoices/INV-2026-001.md", "/.invoices/INV-2026-001.pdf"} {
		if _, err := os.Stat(dir + f); err != nil {
			t.Fatal(err)
		}
	}
	if out := mustRun(t, "today"); !strings.Contains(out, "Overdue invoices") || !strings.Contains(out, "INV-2026-001") {
		t.Fatalf("today:\n%s", out)
	}
	if out := mustRun(t, "stats", "--year", "2026"); !strings.Contains(out, "Invoices") || !regexp.MustCompile(`Unpaid now\s+1\s+1,200.00 GBP`).MatchString(out) {
		t.Fatalf("stats:\n%s", out)
	}
	if out := mustRun(t, "invoice", "paid", "INV-2026-001", "--date", "2026-01-20"); out != "INV-2026-001 paid on 2026-01-20\n" {
		t.Fatalf("paid: %q", out)
	}
	if out := mustRun(t, "today"); strings.Contains(out, "Overdue invoices") {
		t.Fatalf("a paid invoice is not overdue:\n%s", out)
	}
	if _, err := run(t, "invoice", "edit", "INV-2026-001"); err == nil {
		t.Fatal("an issued invoice cannot be edited")
	}
}

func TestCreditNoteFromTheCLI(t *testing.T) {
	setup(t)
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	data, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(data, []byte("business:\n  name: Steve Ltd\n  address: [1 My Street]\n  vat_number: GB999999973\n")...), 0o644)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street")
	mustRun(t, "invoice", "new", "acme", "--line", "Workshop=1000")
	mustRun(t, "invoice", "issue", "acme", "--date", "2026-10-01")

	out := mustRun(t, "invoice", "credit", "INV-2026-001", "--line", "Discount=250")
	if !strings.Contains(out, "Drafted draft-credit-inv-2026-001 against INV-2026-001") || !strings.Contains(out, "mavis invoice issue draft-credit-inv-2026-001") {
		t.Fatalf("credit:\n%s", out)
	}
	out = mustRun(t, "invoice", "issue", "draft-credit")
	if !strings.HasPrefix(out, "Issued CN-2026-001 to Acme Ltd, crediting INV-2026-001: 300.00 GBP") {
		t.Fatalf("issue credit: %q", out)
	}
	out = mustRun(t, "invoice", "list")
	if !regexp.MustCompile(`INV-2026-001\s+issued.*1,200.00 GBP\s+900.00`).MatchString(out) || !strings.Contains(out, "CN-2026-001 (INV-2026-001)") {
		t.Fatalf("list:\n%s", out)
	}
	if out := mustRun(t, "invoice", "show", "INV-2026-001"); !strings.Contains(out, "Credited 300.00; 900.00 still owed") {
		t.Fatalf("show:\n%s", out)
	}
}

func TestQuoteToEngagementToInvoice(t *testing.T) {
	dir := setup(t)
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	data, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(data, []byte("business:\n  name: Steve Ltd\n  address: [1 My Street]\n  vat_number: GB999999973\n")...), 0o644)
	mustRun(t, "client", "add", "globex", "--name", "Globex Corporation", "--status", "prospect")

	out := mustRun(t, "quote", "new", "globex", "--title", "Reporting rebuild", "--scope", "A rebuilt reporting module.", "--line", "Build=10 x 650 day")
	if !strings.Contains(out, "Drafted draft-globex: Reporting rebuild") || !strings.Contains(out, "6,500.00") {
		t.Fatalf("new:\n%s", out)
	}
	out = mustRun(t, "quote", "send", "globex", "--date", "2026-10-01")
	if !strings.HasPrefix(out, "Sent Q-2026-001 to Globex Corporation: 6,500.00 GBP net, valid until 2026-10-31\n") {
		t.Fatalf("send: %q", out)
	}
	if _, err := os.Stat(dir + "/.invoices/Q-2026-001.pdf"); err != nil {
		t.Fatal(err)
	}
	if out := mustRun(t, "quote", "show", "Q-2026-001"); !strings.Contains(out, "A rebuilt reporting module.") {
		t.Fatalf("show:\n%s", out)
	}
	if out := mustRun(t, "today"); !strings.Contains(out, "Quotes waiting") || !strings.Contains(out, "Q-2026-001") {
		t.Fatalf("today:\n%s", out)
	}

	out = mustRun(t, "quote", "accept", "Q-2026-001", "--engagement", "reporting", "--basis", "day", "--rate", "650", "--date", "2026-10-03")
	if !strings.Contains(out, "Q-2026-001 accepted on 2026-10-03") || !strings.Contains(out, "Started Reporting rebuild (globex-reporting), day @ 650.00") {
		t.Fatalf("accept:\n%s", out)
	}
	mustRun(t, "client", "active", "globex")
	mustRun(t, "time", "reporting", "2d", "--date", "2026-10-06")
	if out := mustRun(t, "invoice", "new", "globex", "--month", "2026-10"); !strings.Contains(out, "Reporting rebuild, October 2026") {
		t.Fatalf("the accepted quote's work should invoice:\n%s", out)
	}
	if out := mustRun(t, "stats", "--month", "2026-10"); !strings.Contains(out, "Quotes, net of VAT") || !regexp.MustCompile(`Accepted\s+1\s+6,500.00 GBP`).MatchString(out) {
		t.Fatalf("stats:\n%s", out)
	}
	if _, err := run(t, "quote", "decline", "Q-2026-001", "--date", "2026-10-04"); err == nil {
		t.Fatal("a quote cannot be answered twice")
	}
}

func TestEInvoiceOnceOptedIn(t *testing.T) {
	dir := setup(t)
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	base, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(base, []byte("business:\n  name: Steve Ltd\n  address: [1 My Street, Leeds LS1 1AA]\n  vat_number: GB999999973\n  peppol_id: 9932:GB999999973\n")...), 0o644)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street", "--address", "Manchester M1 1AA")
	mustRun(t, "invoice", "new", "acme", "--line", "Workshop=1000")

	out := mustRun(t, "invoice", "issue", "acme")
	if !strings.Contains(out, "No e-invoice yet; Peppol needs:") || !strings.Contains(out, "PEPPOL-EN16931-R003") {
		t.Fatalf("opted in, with gaps:\n%s", out)
	}
	if _, err := run(t, "invoice", "ubl", "INV-2026-001"); err == nil {
		t.Fatal("ubl should fail while things are missing")
	}

	if _, err := run(t, "client", "set", "acme", "--peppol-id", "GB123"); err == nil {
		t.Fatal("a Peppol ID without a scheme should be refused")
	}
	mustRun(t, "client", "set", "acme", "--peppol-id", "9932:GB123456789", "--buyer-reference", "PO-4471")
	out = mustRun(t, "invoice", "ubl", "INV-2026-001")
	if !strings.HasSuffix(strings.TrimSpace(out), ".invoices/INV-2026-001.xml") {
		t.Fatalf("ubl: %q", out)
	}
	data, _ := os.ReadFile(dir + "/.invoices/INV-2026-001.xml")
	for _, want := range []string{"<cbc:BuyerReference>PO-4471</cbc:BuyerReference>", `schemeID="9932">GB123456789<`, "<cbc:PostalZone>M1 1AA</cbc:PostalZone>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s", want)
		}
	}

	// The next invoice, issued with everything set, gets its e-invoice at once.
	mustRun(t, "invoice", "new", "acme", "--line", "More=500")
	if out := mustRun(t, "invoice", "issue", "acme"); !strings.Contains(out, ".invoices/INV-2026-002.xml") {
		t.Fatalf("issue with everything set:\n%s", out)
	}
}

func TestRetainersFromTheCLI(t *testing.T) {
	setup(t)
	mustRun(t, "client", "add", "globex")
	mustRun(t, "engagement", "add", "globex", "care", "--title", "Care plan", "--basis", "retainer", "--rate", "800", "--start", "2026-01-10")

	// Months are due once over; how many depends on today, so check the
	// shape rather than the count.
	out := mustRun(t, "invoice", "retainers")
	if !strings.Contains(out, "Care plan") || !strings.Contains(out, "January 2026") || !strings.Contains(out, "--draft") {
		t.Fatalf("list:\n%s", out)
	}
	if out := mustRun(t, "today"); !strings.Contains(out, "Retainers to bill") {
		t.Fatalf("today:\n%s", out)
	}
	out = mustRun(t, "invoice", "retainers", "--draft")
	if !strings.Contains(out, "Drafted draft-globex-care-2026-01: 960.00 GBP") {
		t.Fatalf("draft:\n%s", out)
	}
	if out := mustRun(t, "invoice", "retainers"); out != "No retainer months to bill.\n" {
		t.Fatalf("after drafting: %q", out)
	}
}

func TestRemindersFromTheCLI(t *testing.T) {
	setup(t)
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	data, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(data, []byte("business:\n  name: Steve McDougall\n  address: [1 My Street]\n  vat_number: GB999999973\n")...), 0o644)
	mustRun(t, "client", "add", "acme", "--name", "Acme Ltd", "--contact", "Jo Bloggs")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street")
	mustRun(t, "invoice", "new", "acme", "--line", "Workshop=1000")
	mustRun(t, "invoice", "issue", "acme", "--date", "2026-01-05")

	if out := mustRun(t, "today"); !strings.Contains(out, "not chased yet; reminder 1 due: mavis invoice remind INV-2026-001") {
		t.Fatalf("today before:\n%s", out)
	}

	out := mustRun(t, "invoice", "remind", "INV-2026-001")
	for _, want := range []string{"Subject: Invoice INV-2026-001: a reminder", "Hi Jo,", "£1,200.00", "Thanks,\nSteve McDougall", "Attach: ", "INV-2026-001.pdf", "--sent"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview missing %q:\n%s", want, out)
		}
	}
	// A preview logs nothing.
	if out := mustRun(t, "client", "show", "acme"); strings.Contains(out, "Payment reminder") {
		t.Fatal("a preview must not be logged")
	}

	out = mustRun(t, "invoice", "remind", "INV-2026-001", "--sent")
	if !strings.HasPrefix(out, "Logged reminder 1 for INV-2026-001: log/") {
		t.Fatalf("sent: %q", out)
	}
	if out := mustRun(t, "client", "show", "acme"); !strings.Contains(out, "Payment reminder 1 for INV-2026-001") {
		t.Fatalf("not in the client's log:\n%s", out)
	}
	if out := mustRun(t, "today"); !strings.Contains(out, "reminded ") || !strings.Contains(out, "; next from ") {
		t.Fatalf("today after:\n%s", out)
	}
	// The next one is the second, whenever it is sent.
	if out := mustRun(t, "invoice", "remind", "INV-2026-001"); !strings.Contains(out, "second reminder") {
		t.Fatalf("next:\n%s", out)
	}
}

func TestABackdatedReminderIsTrueOnItsDay(t *testing.T) {
	dir := setup(t)
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	data, _ := os.ReadFile(cfg)
	os.WriteFile(cfg, append(data, []byte("business:\n  name: Steve\n  address: [1 My Street]\n  vat_number: GB999999973\n")...), 0o644)
	mustRun(t, "client", "add", "acme")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street")
	mustRun(t, "invoice", "new", "acme", "--line", "Workshop=1000")
	mustRun(t, "invoice", "issue", "acme", "--date", "2026-01-05") // due 2026-02-04

	// As of the day after it fell due, it was 1 day overdue, whatever today is.
	if out := mustRun(t, "invoice", "remind", "INV-2026-001", "--date", "2026-02-05"); !strings.Contains(out, "is now 1 day overdue") {
		t.Fatalf("preview as of the date:\n%s", out)
	}
	out := mustRun(t, "invoice", "remind", "INV-2026-001", "--sent", "--date", "2026-02-05")
	logged, err := os.ReadFile(dir + "/" + strings.TrimSpace(strings.TrimPrefix(out, "Logged reminder 1 for INV-2026-001: ")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"date: 2026-02-05T00:00", "Payment reminder 1 for INV-2026-001: 1,200.00 GBP, 1 day overdue.", "> A quick reminder that", "is now 1 day overdue"} {
		if !strings.Contains(string(logged), want) {
			t.Errorf("log entry missing %q:\n%s", want, logged)
		}
	}
}

func TestLayoutFromConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("MAVIS_ROOT", "")
	cfg := os.Getenv("XDG_CONFIG_HOME") + "/mavis/config.yaml"
	os.MkdirAll(os.Getenv("XDG_CONFIG_HOME")+"/mavis", 0o755)
	os.WriteFile(cfg, []byte("layout:\n  clients: CRM/People\n  log: CRM/Log\n  files: .billing\nbusiness:\n  name: Steve\n  address: [1 My Street]\n  vat_number: GB999999973\n"), 0o644)

	dir := t.TempDir()
	mustRun(t, "init", dir)
	for _, d := range []string{"CRM/People", "CRM/Log", "engagements", "invoices"} {
		if _, err := os.Stat(dir + "/" + d); err != nil {
			t.Errorf("init should create %s: %v", d, err)
		}
	}
	mustRun(t, "client", "add", "acme")
	mustRun(t, "client", "set", "acme", "--address", "1 High Street")
	if _, err := os.Stat(dir + "/CRM/People/acme.md"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "invoice", "new", "acme", "--line", "Work=100")
	out := mustRun(t, "invoice", "issue", "acme")
	if !strings.Contains(out, "/.billing/INV-") {
		t.Fatalf("the PDF should go to the layout's files folder:\n%s", out)
	}

	os.WriteFile(cfg, []byte("root: "+dir+"\nlayout:\n  clients: ../outside\n"), 0o644)
	if _, err := run(t, "client", "list"); err == nil || !strings.Contains(err.Error(), "stay inside it") {
		t.Fatalf("a bad layout should be refused: %v", err)
	}
}

func TestInitAndGit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("MAVIS_ROOT", "")
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	// A new folder gets a repository.
	fresh := t.TempDir() + "/business"
	if out := mustRun(t, "init", fresh); !strings.Contains(out, "Started a git repository there") {
		t.Fatalf("new folder:\n%s", out)
	}
	if _, err := os.Stat(fresh + "/.git"); err != nil {
		t.Fatal("no repository was started")
	}

	// A folder with files in it, not a repository, is left alone.
	used := t.TempDir()
	os.WriteFile(used+"/notes.txt", []byte("mine"), 0o644)
	if out := mustRun(t, "init", "--force", used); strings.Contains(out, "git") {
		t.Fatalf("an existing folder should not get git:\n%s", out)
	}
	if _, err := os.Stat(used + "/.git"); err == nil {
		t.Fatal("git init in a folder that already had files")
	}

	// Inside a repository: refused without a terminal unless --yes, and
	// nothing is created by the refusal.
	repo := t.TempDir()
	os.MkdirAll(repo+"/.git", 0o755)
	os.WriteFile(repo+"/go.mod", []byte("module example\n"), 0o644)
	_, err := run(t, "init", "--force", repo+"/records")
	if err == nil || !strings.Contains(err.Error(), "is inside the git repository at "+repo) || !strings.Contains(err.Error(), "holds code (go.mod)") || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("inside a repository: %v", err)
	}
	if _, err := os.Stat(repo + "/records"); err == nil {
		t.Fatal("a refusal must create nothing")
	}
	out := mustRun(t, "init", "--force", "--yes", repo+"/records")
	if !strings.Contains(out, "Tracked by the git repository at "+repo) {
		t.Fatalf("--yes:\n%s", out)
	}
	if _, err := os.Stat(repo + "/records/.git"); err == nil {
		t.Fatal("no nested repository inside an existing one")
	}
}

func TestCompletionInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", home+"/data")
	t.Setenv("XDG_CONFIG_HOME", home+"/config")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("PATH", "/nonexistent") // no zsh to ask, wherever this runs

	out := mustRun(t, "completion", "install", "bash")
	path := home + "/data/bash-completion/completions/mavis"
	if !strings.HasPrefix(out, "Installed bash completions to "+path) {
		t.Fatalf("bash: %q", out)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "__complete") {
		t.Fatal("not a bash completion script")
	}

	mustRun(t, "completion", "install", "fish")
	if _, err := os.Stat(home + "/config/fish/completions/mavis.fish"); err != nil {
		t.Fatal(err)
	}

	// zsh with the folder not on its fpath: says what to add and leaves
	// ~/.zshrc alone without a terminal, adds it with --yes.
	out = mustRun(t, "completion", "install", "zsh")
	if !strings.Contains(out, "is not on zsh's fpath") || !strings.Contains(out, "fpath=("+home+"/data/zsh/site-functions $fpath)") {
		t.Fatalf("zsh: %q", out)
	}
	if _, err := os.Stat(home + "/.zshrc"); err == nil {
		t.Fatal("~/.zshrc must not be touched without --yes")
	}
	mustRun(t, "completion", "install", "zsh", "--yes")
	if rc, _ := os.ReadFile(home + "/.zshrc"); !strings.Contains(string(rc), "# Added by mavis completion install") {
		t.Fatalf(".zshrc: %q", rc)
	}

	if out := mustRun(t, "completion", "uninstall", "bash"); !strings.HasPrefix(out, "Removed "+path) {
		t.Fatalf("uninstall: %q", out)
	}
	if out := mustRun(t, "completion", "uninstall", "bash"); !strings.Contains(out, "No bash completions installed") {
		t.Fatalf("uninstall twice: %q", out)
	}
	t.Setenv("SHELL", "/bin/tcsh")
	if _, err := run(t, "completion", "install"); err == nil || !strings.Contains(err.Error(), "name it") {
		t.Fatalf("unknown shell: %v", err)
	}
}
