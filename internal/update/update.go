// Package update finds the latest gitmaster release on GitHub and replaces
// the running binary with it.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the GitHub repository releases are published on.
const Repo = "botenvouwer/gitmaster"

// NoUpdateEnv, when set, switches the check at start off. Restart sets it so
// a freshly updated process never updates again in a loop.
const NoUpdateEnv = "GITMASTER_NO_UPDATE"

// apiBase is the GitHub API root. GITMASTER_UPDATE_API overrides it (for
// testing against a local server).
var apiBase = func() string {
	if u := os.Getenv("GITMASTER_UPDATE_API"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.github.com"
}()

// maxDownload bounds what is read from the network for one file.
const maxDownload = 100 << 20

// Release is a published GitHub release.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Parse reads a release version such as v1.2.3. Development builds
// (v1.2.3-4-gabc1234-dirty, "dev", pseudo versions) are not releases.
func Parse(s string) (v [3]int, ok bool) {
	m := releaseVersion.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return v, false
	}
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

// Newer reports whether latest is a newer release than current. A current
// version that is not a release is never updated.
func Newer(current, latest string) bool {
	c, ok := Parse(current)
	if !ok {
		return false
	}
	l, ok := Parse(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// AssetName is the release file for the running platform.
func AssetName() string { return "gitmaster-" + runtime.GOOS + "-" + runtime.GOARCH }

// Latest asks GitHub for the newest release.
func Latest(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no release published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	if _, ok := Parse(r.Tag); !ok {
		return nil, fmt.Errorf("latest release has an unusable tag %q", r.Tag)
	}
	return &r, nil
}

func (r *Release) asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("%s: larger than %d MB", url, maxDownload>>20)
	}
	return data, nil
}

// Executable is the path of the running binary with symlinks resolved, which
// is the file Install replaces.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Install downloads the release's binary for this platform, checks it against
// the release's checksums.txt and atomically replaces the file at target.
func Install(ctx context.Context, r *Release, target string) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("updating a running binary is not supported on windows")
	}
	name := AssetName()
	bin, ok := r.asset(name)
	if !ok {
		return fmt.Errorf("release %s has no %s", r.Tag, name)
	}
	sums, ok := r.asset("checksums.txt")
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", r.Tag)
	}
	sumData, err := fetch(ctx, sums.URL)
	if err != nil {
		return err
	}
	want, err := checksumFor(sumData, name)
	if err != nil {
		return err
	}
	data, err := fetch(ctx, bin.URL)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("checksum mismatch for %s: refusing to install", name)
	}
	return replace(target, data)
}

// checksumFor finds name in a sha256sum style listing ("<hex>  <file>").
func checksumFor(listing []byte, name string) (string, error) {
	for _, line := range strings.Split(string(listing), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s", name)
}

// replace writes data next to target and renames it over target, so the
// binary is never half written.
func replace(target string, data []byte) error {
	fi, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".gitmaster-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s (try running update with the needed permissions): %w", target, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), fi.Mode().Perm()|0o111); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// CheckTimeout bounds the version check done at every start.
const CheckTimeout = 3 * time.Second

// InstallTimeout bounds a download.
const InstallTimeout = 2 * time.Minute
