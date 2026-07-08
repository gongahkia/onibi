package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const RepoModule = "github.com/gongahkia/onibi"
const SchemaVersion = "1"

var LatestURL = "https://api.github.com/repos/gongahkia/onibi/releases/latest"

type Status string

const (
	StatusCurrent     Status = "current"
	StatusOutdated    Status = "outdated"
	StatusUnavailable Status = "unavailable"
)

type Source string

const (
	SourceLocal  Source = "local"
	SourceGitHub Source = "github"
	SourceNone   Source = "none"
)

type InstallSource string

const (
	InstallSourceSource         InstallSource = "source"
	InstallSourceHomebrewCask   InstallSource = "homebrew-cask"
	InstallSourceReleaseArchive InstallSource = "release-archive"
)

type Options struct {
	CurrentVersion          string
	CurrentCommit           string
	RepoDir                 string
	CheckGitHub             bool
	Client                  *http.Client
	Timeout                 time.Duration
	Executable              string
	InstallSource           string
	ConditionalETag         string
	ConditionalLastModified string
	CachedResult            *Result
	GitHubToken             string
}

type Result struct {
	SchemaVersion  string `json:"schema_version"`
	Status         Status `json:"status"`
	Source         Source `json:"source"`
	CurrentVersion string `json:"current_version"`
	CurrentCommit  string `json:"current_commit"`
	LatestVersion  string `json:"latest_version,omitempty"`
	LatestCommit   string `json:"latest_commit,omitempty"`
	RepoDir        string `json:"repo_dir,omitempty"`
	InstallSource  string `json:"install_source,omitempty"`
	PackageState   string `json:"package_state,omitempty"`
	URL            string `json:"url,omitempty"`
	ETag           string `json:"etag,omitempty"`
	LastModified   string `json:"last_modified,omitempty"`
	Detail         string `json:"detail"`
	Command        string `json:"command,omitempty"`
}

func (r Result) Outdated() bool { return r.Status == StatusOutdated }

func Check(ctx context.Context, opts Options) Result {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	if strings.TrimSpace(opts.CurrentVersion) == "" {
		opts.CurrentVersion = "unknown"
	}
	if strings.TrimSpace(opts.CurrentCommit) == "" {
		opts.CurrentCommit = "unknown"
	}
	if repo, ok := findRepo(opts.RepoDir); ok {
		return normalizeResult(checkLocal(ctx, opts, repo))
	}
	if opts.CheckGitHub {
		return normalizeResult(checkGitHub(ctx, opts))
	}
	return normalizeResult(Result{Status: StatusUnavailable, Source: SourceNone, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, Detail: "no local Onibi checkout found"})
}

func normalizeResult(res Result) Result {
	if res.SchemaVersion == "" {
		res.SchemaVersion = SchemaVersion
	}
	return res
}

func checkLocal(ctx context.Context, opts Options, repo string) Result {
	res := Result{Source: SourceLocal, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, RepoDir: repo, InstallSource: string(InstallSourceSource)}
	head, err := gitOutput(ctx, repo, "rev-parse", "--short=12", "HEAD")
	if err != nil {
		res.Status = StatusUnavailable
		res.Detail = "local source unavailable: " + err.Error()
		return res
	}
	res.LatestCommit = strings.TrimSpace(head)
	if commitMatches(opts.CurrentCommit, res.LatestCommit) {
		res.Status = StatusCurrent
		res.Detail = "current commit matches local source " + res.LatestCommit
		return res
	}
	if !knownCommit(opts.CurrentCommit) {
		res.Status = StatusUnavailable
		res.Detail = "current build commit unknown; local source is " + res.LatestCommit
		res.Command = sourceUpdateCommand(repo)
		return res
	}
	res.Status = StatusOutdated
	res.Detail = "local source " + res.LatestCommit + " differs from installed " + opts.CurrentCommit
	res.Command = sourceUpdateCommand(repo)
	return res
}

func checkGitHub(ctx context.Context, opts Options) Result {
	installSource := detectInstallSource(ctx, opts)
	res := Result{Source: SourceGitHub, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, InstallSource: string(installSource), URL: LatestURL}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, LatestURL, nil)
	if err != nil {
		res.Status = StatusUnavailable
		res.Detail = err.Error()
		return res
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "onibi-update-check")
	if token := githubToken(opts); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if strings.TrimSpace(opts.ConditionalETag) != "" {
		req.Header.Set("If-None-Match", strings.TrimSpace(opts.ConditionalETag))
	}
	if strings.TrimSpace(opts.ConditionalLastModified) != "" {
		req.Header.Set("If-Modified-Since", strings.TrimSpace(opts.ConditionalLastModified))
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusUnavailable
		res.Detail = "github release check failed: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return cachedGitHubResult(opts, res, resp)
	}
	if resp.StatusCode != http.StatusOK {
		res.Status = StatusUnavailable
		res.Detail = fmt.Sprintf("github release check failed: HTTP %d", resp.StatusCode)
		return res
	}
	res.ETag = strings.TrimSpace(resp.Header.Get("ETag"))
	res.LastModified = strings.TrimSpace(resp.Header.Get("Last-Modified"))
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		res.Status = StatusUnavailable
		res.Detail = "github release parse failed: " + err.Error()
		return res
	}
	res.LatestVersion = strings.TrimSpace(body.TagName)
	res.URL = strings.TrimSpace(body.HTMLURL)
	cmp, ok := compareVersions(opts.CurrentVersion, res.LatestVersion)
	if !ok {
		res.Status = StatusUnavailable
		res.Detail = "current " + opts.CurrentVersion + " cannot be compared to latest release " + res.LatestVersion
		res.Command = updateCommand(ctx, opts, installSource, res.LatestVersion, res.URL, &res)
		return res
	}
	if cmp >= 0 {
		res.Status = StatusCurrent
		res.Detail = "current " + opts.CurrentVersion + " is up to date with latest release " + res.LatestVersion
		return res
	}
	res.Status = StatusOutdated
	res.Detail = "latest release " + res.LatestVersion + " is newer than " + opts.CurrentVersion
	res.Command = updateCommand(ctx, opts, installSource, res.LatestVersion, res.URL, &res)
	return res
}

func cachedGitHubResult(opts Options, res Result, resp *http.Response) Result {
	if opts.CachedResult == nil || opts.CachedResult.Status == "" {
		res.Status = StatusUnavailable
		res.Detail = "github release not modified but no cached release metadata is available"
		return res
	}
	cached := *opts.CachedResult
	if cached.Source == "" {
		cached.Source = SourceGitHub
	}
	if cached.CurrentVersion == "" {
		cached.CurrentVersion = opts.CurrentVersion
	}
	if cached.CurrentCommit == "" {
		cached.CurrentCommit = opts.CurrentCommit
	}
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		cached.ETag = etag
	} else if cached.ETag == "" {
		cached.ETag = strings.TrimSpace(opts.ConditionalETag)
	}
	if lm := strings.TrimSpace(resp.Header.Get("Last-Modified")); lm != "" {
		cached.LastModified = lm
	} else if cached.LastModified == "" {
		cached.LastModified = strings.TrimSpace(opts.ConditionalLastModified)
	}
	return cached
}

func githubToken(opts Options) string {
	if token := strings.TrimSpace(opts.GitHubToken); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("ONIBI_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func findRepo(start string) (string, bool) {
	if start == "" {
		start = os.Getenv("ONIBI_SOURCE_DIR")
	}
	if start == "" {
		start, _ = os.Getwd()
	}
	if start == "" {
		return "", false
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if isOnibiRepo(abs) {
			return abs, true
		}
		next := filepath.Dir(abs)
		if next == abs {
			return "", false
		}
		abs = next
	}
}

func isOnibiRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "module "+RepoModule {
			return true
		}
	}
	return false
}

var gitOutput = func(ctx context.Context, repo string, args ...string) (string, error) {
	all := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func knownCommit(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s != "" && s != "unknown"
}

func commitMatches(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	if !knownCommit(a) || !knownCommit(b) {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func compareVersions(current, latest string) (int, bool) {
	c, okC := versionParts(current)
	l, okL := versionParts(latest)
	if !okC || !okL {
		return 0, false
	}
	for i := 0; i < len(c) || i < len(l); i++ {
		var cv, lv int
		if i < len(c) {
			cv = c[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if cv > lv {
			return 1, true
		}
		if cv < lv {
			return -1, true
		}
	}
	return 0, true
}

func versionParts(s string) ([]int, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "v"))
	if s == "" {
		return nil, false
	}
	if cut := strings.IndexAny(s, "-+"); cut >= 0 {
		s = s[:cut]
	}
	fields := strings.Split(s, ".")
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			return nil, false
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, len(out) >= 2
}

func updateCommand(ctx context.Context, opts Options, source InstallSource, latestVersion, releaseURL string, res *Result) string {
	switch source {
	case InstallSourceHomebrewCask:
		return homebrewCaskUpdateCommand(ctx, res)
	case InstallSourceReleaseArchive:
		return releaseArchiveUpdateCommand(opts, latestVersion, releaseURL)
	default:
		if repo, ok := findRepo(opts.RepoDir); ok {
			return sourceUpdateCommand(repo)
		}
		return releaseArchiveUpdateCommand(opts, latestVersion, releaseURL)
	}
}

func sourceUpdateCommand(repo string) string {
	return "make -C " + shellQuote(repo) + " install && onibi doctor --after-upgrade --offline && onibi up"
}

func releaseArchiveUpdateCommand(opts Options, latestVersion, releaseURL string) string {
	exe := strings.TrimSpace(opts.Executable)
	if exe == "" {
		if self, err := os.Executable(); err == nil {
			exe = self
		}
	}
	targetOnibi := "$(dirname \"$(command -v onibi)\")/onibi"
	targetNotify := "$(dirname \"$(command -v onibi)\")/onibi-notify"
	if exe != "" {
		dir := filepath.Dir(exe)
		targetOnibi = shellQuote(filepath.Join(dir, "onibi"))
		targetNotify = shellQuote(filepath.Join(dir, "onibi-notify"))
	}
	tag := strings.TrimSpace(latestVersion)
	if tag == "" {
		tag = "latest"
	}
	version := strings.TrimPrefix(tag, "v")
	asset := fmt.Sprintf("onibi_%s_%s_%s.tar.gz", version, runtime.GOOS, archiveArch(runtime.GOARCH))
	base := strings.TrimRight(releaseURL, "/")
	if base == "" || tag == "latest" || strings.Contains(base, "/releases/tag/") {
		if tag == "latest" {
			base = "https://github.com/gongahkia/onibi/releases/latest/download"
		} else {
			base = "https://github.com/gongahkia/onibi/releases/download/" + tag
		}
	} else {
		base = "https://github.com/gongahkia/onibi/releases/download/" + tag
	}
	return "asset=" + shellQuote(asset) + " base=" + shellQuote(base) +
		" tmp=$(mktemp -d) && trap 'rm -rf \"$tmp\"' EXIT" +
		" && curl -fsSL -o \"$tmp/$asset\" \"$base/$asset\"" +
		" && curl -fsSL -o \"$tmp/checksums.txt\" \"$base/checksums.txt\"" +
		" && (cd \"$tmp\" && grep \"  $asset$\" checksums.txt > checksum.line && if command -v shasum >/dev/null 2>&1; then shasum -a 256 -c checksum.line; else sha256sum -c checksum.line; fi)" +
		" && if [ -n \"${ONIBI_RELEASE_GPG_KEY:-}\" ] && command -v gpg >/dev/null 2>&1; then curl -fsSL -o \"$tmp/checksums.txt.sig\" \"$base/checksums.txt.sig\" && printf '%s\\n' \"$ONIBI_RELEASE_GPG_KEY\" | gpg --import && gpg --verify \"$tmp/checksums.txt.sig\" \"$tmp/checksums.txt\"; fi" +
		" && tar -xzf \"$tmp/$asset\" -C \"$tmp\"" +
		" && install -m 0755 \"$tmp/onibi\" " + targetOnibi +
		" && install -m 0755 \"$tmp/onibi-notify\" " + targetNotify +
		" && onibi doctor --after-upgrade --offline"
}

func archiveArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "i386"
	default:
		return goarch
	}
}

func detectInstallSource(ctx context.Context, opts Options) InstallSource {
	switch strings.ToLower(strings.TrimSpace(opts.InstallSource)) {
	case string(InstallSourceSource):
		return InstallSourceSource
	case string(InstallSourceHomebrewCask), "homebrew", "brew", "cask":
		return InstallSourceHomebrewCask
	case string(InstallSourceReleaseArchive), "release", "archive", "tarball":
		return InstallSourceReleaseArchive
	}
	exe := strings.TrimSpace(opts.Executable)
	if exe == "" {
		exe, _ = os.Executable()
	}
	lower := strings.ToLower(filepath.ToSlash(exe))
	if strings.Contains(lower, "/caskroom/onibi/") || strings.Contains(lower, "/cellar/onibi/") || strings.Contains(lower, "/homebrew/cellar/onibi/") {
		return InstallSourceHomebrewCask
	}
	if brewCaskInstalled(ctx) {
		return InstallSourceHomebrewCask
	}
	return InstallSourceReleaseArchive
}

var brewCaskInstalled = func(ctx context.Context) bool {
	state, _ := homebrewCaskStatus(ctx)
	return state != "not-installed" && state != "unavailable"
}

var homebrewCaskStatus = func(ctx context.Context) (string, string) {
	if _, err := exec.LookPath("brew"); err != nil {
		return "unavailable", "brew not found"
	}
	if err := exec.CommandContext(ctx, "brew", "list", "--cask", "onibi").Run(); err != nil {
		return "not-installed", "Homebrew cask not installed"
	}
	if out, err := exec.CommandContext(ctx, "brew", "list", "--pinned").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) == "onibi" {
				return "pinned", "Homebrew package is pinned"
			}
		}
	}
	out, err := exec.CommandContext(ctx, "brew", "outdated", "--json=v2", "--cask", "onibi").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "unavailable", "brew outdated failed: " + err.Error()
	}
	var body struct {
		Casks []struct {
			Name  string `json:"name"`
			Token string `json:"token"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		return "unavailable", "brew outdated parse failed: " + err.Error()
	}
	for _, cask := range body.Casks {
		if cask.Name == "onibi" || cask.Token == "onibi" {
			return "outdated", "Homebrew cask is outdated"
		}
	}
	return "current", "Homebrew cask is current or tap has not published the latest release"
}

func homebrewCaskUpdateCommand(ctx context.Context, res *Result) string {
	state, detail := homebrewCaskStatus(ctx)
	if res != nil {
		res.PackageState = "homebrew-" + state
		if state == "current" && res.Status == StatusOutdated {
			res.Detail += "; Homebrew cask is not outdated yet, so the tap may lag GitHub"
		}
		if state == "pinned" {
			res.Detail += "; " + detail
		}
	}
	base := "brew upgrade --cask gongahkia/onibi/onibi && onibi doctor --after-upgrade --offline"
	switch state {
	case "outdated":
		return base
	case "pinned":
		return "brew unpin onibi 2>/dev/null || true; " + base
	case "current":
		return "brew update && " + base
	case "unavailable":
		return "brew update && " + base
	default:
		return "brew install --cask gongahkia/onibi/onibi && onibi doctor --after-upgrade --offline"
	}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
