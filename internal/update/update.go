// Package update replaces the running mavis with the latest GitHub release.
//
// The archive is checked against the release's checksums.txt before
// anything is written, and the new binary goes in by rename, so a failed
// or interrupted update leaves the old one in place.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxDownload bounds what is read from the network: the binary is ~30MB.
const maxDownload = 200 << 20

// Client finds and fetches releases.
type Client struct {
	API   string // https://api.github.com
	Repo  string // owner/name
	Token string // optional, for the API only: lifts the rate limit
	HTTP  *http.Client
}

// Default is the client for mavis's own releases.
func Default() Client {
	return Client{
		API:   "https://api.github.com",
		Repo:  "JustSteveKing/mavis",
		Token: os.Getenv("GITHUB_TOKEN"),
		HTTP:  &http.Client{Timeout: 2 * time.Minute},
	}
}

// Release is the latest release, as far as an update needs it.
type Release struct {
	Version string            `json:"version"` // without the v
	URL     string            `json:"url"`     // the release page
	Assets  map[string]string `json:"-"`       // file name to download URL
}

// Latest asks GitHub for the newest release.
func (c Client) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+"/repos/"+c.Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s for the latest release", res.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("reading the latest release: %w", err)
	}
	if _, ok := parse(body.TagName); !ok {
		return Release{}, fmt.Errorf("the latest release is tagged %q, which is not a version", body.TagName)
	}
	r := Release{Version: strings.TrimPrefix(body.TagName, "v"), URL: body.HTMLURL, Assets: map[string]string{}}
	for _, a := range body.Assets {
		r.Assets[a.Name] = a.URL
	}
	return r, nil
}

var release = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// parse reads a release version, 0.1.4 or v0.1.4. A local build such as
// 0.1.4-3-gabc1234-dirty, or dev, is not one.
func parse(v string) ([3]int, bool) {
	m := release.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out, true
}

// IsRelease says whether a version is a plain release.
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer says whether latest is a later release than current. Both must be
// releases; a local build compares as not newer, and the caller decides.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// ArchiveName is the release archive for a platform.
func ArchiveName(version, goos, goarch string) string {
	return fmt.Sprintf("mavis_%s_%s_%s.tar.gz", version, goos, goarch)
}

// Fetch downloads this platform's archive, checks it against the release's
// checksums, and returns the mavis binary from inside it.
func (c Client) Fetch(ctx context.Context, r Release, goos, goarch string) ([]byte, error) {
	name := ArchiveName(r.Version, goos, goarch)
	archiveURL, ok := r.Assets[name]
	if !ok {
		return nil, fmt.Errorf("release %s has no build for %s/%s (no %s)", r.Version, goos, goarch, name)
	}
	sumsURL, ok := r.Assets["checksums.txt"]
	if !ok {
		return nil, fmt.Errorf("release %s has no checksums.txt, so the download cannot be checked", r.Version)
	}
	sums, err := c.get(ctx, sumsURL)
	if err != nil {
		return nil, err
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return nil, err
	}
	archive, err := c.get(ctx, archiveURL)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s does not match its checksum; nothing was changed", name)
	}
	return binaryFrom(archive)
}

func (c Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", filepath.Base(url), err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", filepath.Base(url), res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", filepath.Base(url), err)
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("downloading %s: larger than any mavis release should be", filepath.Base(url))
	}
	return data, nil
}

// checksumFor finds a file's SHA-256 in a checksums.txt.
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt does not list %s", name)
}

// binaryFrom takes the mavis binary out of a release archive.
func binaryFrom(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading the archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the archive has no mavis binary in it")
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "mavis" {
			return io.ReadAll(io.LimitReader(tr, maxDownload))
		}
	}
}

// Replace puts binary at path: written beside it, then renamed over it, so
// the old binary stays whole until the new one is complete. Renaming over a
// running executable is fine on Linux and macOS; the running process keeps
// the old file open.
func Replace(path string, binary []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mavis-update-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot write to %s; run it again with the permissions that installed mavis there (for example sudo)", dir)
		}
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()|0o111); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
