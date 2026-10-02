package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeGitHub serves a latest release of 0.2.0 with one linux/amd64 archive.
func fakeGitHub(t *testing.T, sums func(name string, archive []byte) string) (Client, *httptest.Server) {
	t.Helper()
	name := ArchiveName("0.2.0", "linux", "amd64")
	arc := archive(t, map[string]string{"README.md": "readme", "mavis": "new binary"})
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/JustSteveKing/mavis/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Error("the token was not sent to the API")
		}
		fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://example.test/r","assets":[
			{"name":%q,"browser_download_url":"%s/dl/%s"},
			{"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"}]}`, name, srv.URL, name, srv.URL)
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("the token was sent to a download")
		}
		fmt.Fprint(w, sums(name, arc))
	})
	mux.HandleFunc("/dl/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(arc) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Client{API: srv.URL, Repo: "JustSteveKing/mavis", Token: "tok", HTTP: srv.Client()}, srv
}

func goodSums(name string, arc []byte) string {
	sum := sha256.Sum256(arc)
	return "0000  mavis_0.2.0_darwin_arm64.tar.gz\n" + hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func TestFetchesAndChecksTheLatestRelease(t *testing.T) {
	c, _ := fakeGitHub(t, goodSums)
	r, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.2.0" || r.URL != "https://example.test/r" {
		t.Fatalf("got %+v", r)
	}
	bin, err := c.Fetch(context.Background(), r, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if string(bin) != "new binary" {
		t.Errorf("got %q", bin)
	}
}

func TestRefusesADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	c, _ := fakeGitHub(t, func(name string, _ []byte) string {
		return strings.Repeat("ab", 32) + "  " + name + "\n"
	})
	r, _ := c.Latest(context.Background())
	if _, err := c.Fetch(context.Background(), r, "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
}

func TestNoBuildForThisPlatform(t *testing.T) {
	c, _ := fakeGitHub(t, goodSums)
	r, _ := c.Latest(context.Background())
	if _, err := c.Fetch(context.Background(), r, "linux", "riscv64"); err == nil || !strings.Contains(err.Error(), "no build for linux/riscv64") {
		t.Fatalf("got %v", err)
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"0.1.4", "0.1.5", true},
		{"0.1.4", "v0.2.0", true},
		{"0.1.9", "0.1.10", true},
		{"0.1.4", "0.1.4", false},
		{"0.2.0", "0.1.9", false},
		{"0.1.4-3-gabc1234-dirty", "0.1.5", false},
		{"dev", "0.1.5", false},
	} {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.current, tc.latest, got)
		}
	}
}

func TestReplaceSwapsTheFileAndKeepsItExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mavis")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(got) != "new" || info.Mode().Perm()&0o111 == 0 {
		t.Errorf("got %q, mode %v", got, info.Mode())
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".mavis-update-*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}
