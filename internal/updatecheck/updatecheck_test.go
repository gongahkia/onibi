package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		current string
		latest  string
		want    int
		ok      bool
	}{
		{"v1.2.3", "v1.2.4", -1, true},
		{"1.3.0", "v1.2.4", 1, true},
		{"v1.2.3", "1.2.3", 0, true},
		{"v2-dev", "v1.0.0", 0, false},
	} {
		got, ok := compareVersions(tc.current, tc.latest)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("compareVersions(%q,%q) = %d,%v want %d,%v", tc.current, tc.latest, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLocalRepoOutdated(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+RepoModule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "go.mod")
	runGit(t, repo, "commit", "-m", "init")
	head := strings.TrimSpace(runGit(t, repo, "rev-parse", "--short=12", "HEAD"))
	res := Check(context.Background(), Options{CurrentVersion: "v1.0.0", CurrentCommit: "0000000", RepoDir: repo})
	if res.Status != StatusOutdated || res.Source != SourceLocal || res.LatestCommit != head {
		t.Fatalf("result = %#v", res)
	}
	if !strings.Contains(res.Command, "make -C") {
		t.Fatalf("command = %q", res.Command)
	}
	if !strings.Contains(res.Command, "doctor --after-upgrade --offline") {
		t.Fatalf("command missing after-upgrade doctor: %q", res.Command)
	}
}

func TestLocalRepoCurrent(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+RepoModule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldGitOutput := gitOutput
	t.Cleanup(func() { gitOutput = oldGitOutput })
	gitOutput = func(context.Context, string, ...string) (string, error) { return "abcdef123456\n", nil }
	res := Check(context.Background(), Options{CurrentVersion: "v1.0.0", CurrentCommit: "abcdef1", RepoDir: repo})
	if res.Status != StatusCurrent {
		t.Fatalf("result = %#v", res)
	}
}

func TestGitHubReleaseOutdated(t *testing.T) {
	oldStatus := homebrewCaskStatus
	homebrewCaskStatus = func(context.Context) (string, string) { return "outdated", "outdated" }
	t.Cleanup(func() { homebrewCaskStatus = oldStatus })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Fatalf("missing GitHub API version header")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://example.com/release"}`))
	}))
	t.Cleanup(srv.Close)
	oldURL := LatestURL
	t.Cleanup(func() { LatestURL = oldURL })
	LatestURL = srv.URL
	res := Check(context.Background(), Options{CurrentVersion: "v1.1.0", CurrentCommit: "abc", RepoDir: filepath.Join(t.TempDir(), "missing"), CheckGitHub: true, Client: srv.Client(), InstallSource: string(InstallSourceHomebrewCask)})
	if res.Status != StatusOutdated || res.Source != SourceGitHub || res.LatestVersion != "v1.2.0" {
		t.Fatalf("result = %#v", res)
	}
	if res.InstallSource != string(InstallSourceHomebrewCask) || !strings.Contains(res.Command, "brew upgrade --cask") {
		t.Fatalf("install source/command = %q %q", res.InstallSource, res.Command)
	}
	if res.PackageState != "homebrew-outdated" {
		t.Fatalf("package state = %q", res.PackageState)
	}
}

func TestGitHubConditionalNotModifiedUsesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"abc"` {
			t.Fatalf("If-None-Match = %q", r.Header.Get("If-None-Match"))
		}
		if r.Header.Get("If-Modified-Since") != "Mon, 01 Jan 2024 00:00:00 GMT" {
			t.Fatalf("If-Modified-Since = %q", r.Header.Get("If-Modified-Since"))
		}
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	oldURL := LatestURL
	t.Cleanup(func() { LatestURL = oldURL })
	LatestURL = srv.URL
	cached := Result{
		Status:         StatusCurrent,
		Source:         SourceGitHub,
		CurrentVersion: "v1.2.0",
		CurrentCommit:  "abc",
		LatestVersion:  "v1.2.0",
		Detail:         "cached current",
		ETag:           `"abc"`,
		LastModified:   "Mon, 01 Jan 2024 00:00:00 GMT",
	}
	res := Check(context.Background(), Options{
		CurrentVersion:          "v1.2.0",
		CurrentCommit:           "abc",
		RepoDir:                 filepath.Join(t.TempDir(), "missing"),
		CheckGitHub:             true,
		Client:                  srv.Client(),
		ConditionalETag:         cached.ETag,
		ConditionalLastModified: cached.LastModified,
		CachedResult:            &cached,
	})
	if res.Status != StatusCurrent || res.Detail != "cached current" || res.ETag != `"abc"` {
		t.Fatalf("result = %#v", res)
	}
}

func TestGitHubHomebrewTapLagState(t *testing.T) {
	oldStatus := homebrewCaskStatus
	homebrewCaskStatus = func(context.Context) (string, string) { return "current", "tap current" }
	t.Cleanup(func() { homebrewCaskStatus = oldStatus })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://example.com/release"}`))
	}))
	t.Cleanup(srv.Close)
	oldURL := LatestURL
	t.Cleanup(func() { LatestURL = oldURL })
	LatestURL = srv.URL
	res := Check(context.Background(), Options{CurrentVersion: "v1.1.0", CurrentCommit: "abc", RepoDir: filepath.Join(t.TempDir(), "missing"), CheckGitHub: true, Client: srv.Client(), InstallSource: string(InstallSourceHomebrewCask)})
	if res.PackageState != "homebrew-current" || !strings.Contains(res.Detail, "tap may lag GitHub") || !strings.Contains(res.Command, "brew update && brew upgrade --cask") {
		t.Fatalf("result = %#v", res)
	}
}

func TestGitHubReleaseArchiveCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://example.com/release"}`))
	}))
	t.Cleanup(srv.Close)
	oldURL := LatestURL
	t.Cleanup(func() { LatestURL = oldURL })
	LatestURL = srv.URL
	exe := filepath.Join(t.TempDir(), "onibi")
	res := Check(context.Background(), Options{
		CurrentVersion: "v1.1.0",
		CurrentCommit:  "abc",
		RepoDir:        filepath.Join(t.TempDir(), "missing"),
		CheckGitHub:    true,
		Client:         srv.Client(),
		Executable:     exe,
		InstallSource:  string(InstallSourceReleaseArchive),
	})
	if res.InstallSource != string(InstallSourceReleaseArchive) {
		t.Fatalf("install source = %q", res.InstallSource)
	}
	for _, want := range []string{"curl -fsSL", "onibi_1.2.0_", "checksums.txt", "shasum -a 256 -c", "ONIBI_RELEASE_GPG_KEY", "install -m 0755", filepath.Join(filepath.Dir(exe), "onibi-notify")} {
		if !strings.Contains(res.Command, want) {
			t.Fatalf("command missing %q: %q", want, res.Command)
		}
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
