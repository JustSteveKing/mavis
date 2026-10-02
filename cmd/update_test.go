package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JustSteveKing/mavis/internal/update"
)

// runAs runs mavis as a given version against a fake GitHub whose latest
// release is 0.2.0. Nothing here reaches the install step: that would
// replace the test binary.
func runAs(t *testing.T, version string, args ...string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.2.0","html_url":"https://example.test/r","assets":[]}`)
	}))
	t.Cleanup(srv.Close)
	old := newUpdateClient
	newUpdateClient = func() update.Client {
		return update.Client{API: srv.URL, Repo: "JustSteveKing/mavis", HTTP: srv.Client()}
	}
	t.Cleanup(func() { newUpdateClient = old })

	root := NewRootCommand(version)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestUpdateCheck(t *testing.T) {
	for _, tc := range []struct{ version, want string }{
		{"0.1.4", "mavis 0.2.0 is out; you have 0.1.4."},
		{"0.2.0", "mavis 0.2.0 is the latest release."},
		{"0.1.4-3-gabc1234-dirty", "is a local build (0.1.4-3-gabc1234-dirty), not a release"},
	} {
		if got := runAs(t, tc.version, "update", "--check"); !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q", tc.version, got)
		}
	}
}

func TestUpdateLeavesALocalBuildAlone(t *testing.T) {
	got := runAs(t, "dev", "update")
	if !strings.Contains(got, "mavis update --force") {
		t.Errorf("got %q", got)
	}
}

func TestUpdateCheckJSON(t *testing.T) {
	got := runAs(t, "0.1.4", "update", "--check", "--json")
	if !strings.Contains(got, `"update_available": true`) || !strings.Contains(got, `"latest": "0.2.0"`) {
		t.Errorf("got %s", got)
	}
}
