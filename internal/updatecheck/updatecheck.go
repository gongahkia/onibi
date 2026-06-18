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
	"strconv"
	"strings"
	"time"
)

const RepoModule = "github.com/gongahkia/onibi"

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

type Options struct {
	CurrentVersion string
	CurrentCommit  string
	RepoDir        string
	CheckGitHub    bool
	Client         *http.Client
	Timeout        time.Duration
}

type Result struct {
	Status         Status `json:"status"`
	Source         Source `json:"source"`
	CurrentVersion string `json:"current_version"`
	CurrentCommit  string `json:"current_commit"`
	LatestVersion  string `json:"latest_version,omitempty"`
	LatestCommit   string `json:"latest_commit,omitempty"`
	RepoDir        string `json:"repo_dir,omitempty"`
	URL            string `json:"url,omitempty"`
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
		return checkLocal(ctx, opts, repo)
	}
	if opts.CheckGitHub {
		return checkGitHub(ctx, opts)
	}
	return Result{Status: StatusUnavailable, Source: SourceNone, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, Detail: "no local Onibi checkout found"}
}

func checkLocal(ctx context.Context, opts Options, repo string) Result {
	res := Result{Source: SourceLocal, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, RepoDir: repo}
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
		res.Command = "make -C " + shellQuote(repo) + " install && onibi up"
		return res
	}
	res.Status = StatusOutdated
	res.Detail = "local source " + res.LatestCommit + " differs from installed " + opts.CurrentCommit
	res.Command = "make -C " + shellQuote(repo) + " install && onibi up"
	return res
}

func checkGitHub(ctx context.Context, opts Options) Result {
	res := Result{Source: SourceGitHub, CurrentVersion: opts.CurrentVersion, CurrentCommit: opts.CurrentCommit, URL: LatestURL}
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
	resp, err := client.Do(req)
	if err != nil {
		res.Status = StatusUnavailable
		res.Detail = "github release check failed: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.Status = StatusUnavailable
		res.Detail = fmt.Sprintf("github release check failed: HTTP %d", resp.StatusCode)
		return res
	}
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
		res.Command = updateCommand()
		return res
	}
	if cmp >= 0 {
		res.Status = StatusCurrent
		res.Detail = "current " + opts.CurrentVersion + " is up to date with latest release " + res.LatestVersion
		return res
	}
	res.Status = StatusOutdated
	res.Detail = "latest release " + res.LatestVersion + " is newer than " + opts.CurrentVersion
	res.Command = updateCommand()
	return res
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

func updateCommand() string {
	return "brew upgrade --cask gongahkia/onibi/onibi or make -C <onibi repo> install && onibi up"
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
