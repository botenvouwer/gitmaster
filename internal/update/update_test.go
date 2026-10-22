package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseAndNewer(t *testing.T) {
	for _, tc := range []struct {
		cur, latest string
		want        bool
	}{
		{"v0.1.0", "v0.1.1", true},
		{"v0.1.9", "v0.2.0", true},
		{"v0.9.0", "v1.0.0", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.2.0", "v1.1.9", false},
		{"v0.10.0", "v0.9.0", false}, // numeric, not lexical
		{"dev", "v9.9.9", false},
		{"v0.1.0-3-gabc1234-dirty", "v0.2.0", false},
		{"v0.0.0-20260101000000-abcdef123456", "v0.2.0", false},
		{"v0.1.0", "latest", false},
	} {
		if got := Newer(tc.cur, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.cur, tc.latest, got, tc.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	listing := []byte("AAA111  gitmaster-linux-amd64\nbbb222 *gitmaster-darwin-arm64\n")
	if got, err := checksumFor(listing, "gitmaster-linux-amd64"); err != nil || got != "aaa111" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := checksumFor(listing, "gitmaster-darwin-arm64"); err != nil || got != "bbb222" {
		t.Errorf("binary-mode marker: got %q, %v", got, err)
	}
	if _, err := checksumFor(listing, "gitmaster-windows-amd64"); err == nil {
		t.Error("a missing file must be an error")
	}
}

// release serves a fake GitHub with one release whose binary is body and
// whose checksum is sum (computed from body when empty).
func release(t *testing.T, tag, body, sum string) *Release {
	t.Helper()
	if sum == "" {
		s := sha256.Sum256([]byte(body))
		sum = hex.EncodeToString(s[:])
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/bin", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, "%s  %s\n", sum, AssetName()) })
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
			tag, AssetName(), srv.URL, srv.URL)
	})
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
	r, err := Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInstallReplacesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not supported on windows")
	}
	r := release(t, "v1.2.3", "new binary", "")
	if r.Tag != "v1.2.3" {
		t.Fatalf("tag %q", r.Tag)
	}
	target := filepath.Join(t.TempDir(), "gitmaster")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), r, target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new binary" {
		t.Errorf("target holds %q", got)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm()&0o111 == 0 {
		t.Error("updated binary is not executable")
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".gitmaster-update-*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestInstallRefusesBadChecksum(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not supported on windows")
	}
	r := release(t, "v1.2.3", "tampered", strings.Repeat("0", 64))
	target := filepath.Join(t.TempDir(), "gitmaster")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Install(context.Background(), r, target)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old binary" {
		t.Errorf("a failed update must leave the binary alone, got %q", got)
	}
}

func TestLatestWithoutRelease(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()
	if _, err := Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "no release") {
		t.Errorf("got %v", err)
	}
}
