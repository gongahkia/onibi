package devtool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const manifestName = ".onibi-install.json"

type Manifest struct {
	Schema      int               `json:"schema"`
	Source      string            `json:"source"`
	Version     string            `json:"version"`
	Commit      string            `json:"commit"`
	InstalledAt time.Time         `json:"installedAt"`
	Files       map[string]string `json:"files"`
}
type LifecycleOptions struct {
	DryRun, Pull, RestartWeb bool
	Home                     string
}

func (t *Tool) prefix() error {
	if t.Prefix == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		t.Prefix = filepath.Join(home, ".local", "bin")
	}
	prefix, err := filepath.Abs(t.Prefix)
	if err != nil {
		return err
	}
	if prefix == string(filepath.Separator) || strings.ContainsAny(prefix, "\x00\r\n") {
		return errors.New("choose a normal user binary directory for --prefix")
	}
	if filepath.Clean(prefix) == filepath.Join(t.Root, "bin") {
		return errors.New("install prefix must differ from the checkout's bin build directory")
	}
	t.Prefix = prefix
	return nil
}
func digest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing non-regular managed file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (t *Tool) manifest() (Manifest, bool, error) {
	path := filepath.Join(t.Prefix, manifestName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return Manifest{}, false, errors.New("invalid installation manifest")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, false, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, false, err
	}
	if m.Schema != 1 || len(m.Files) != len(binaryNames) {
		return m, false, errors.New("unrecognized installation manifest")
	}
	for _, name := range binaryNames {
		if len(m.Files[name]) != 64 {
			return m, false, errors.New("incomplete installation manifest")
		}
	}
	return m, true, nil
}
func (t *Tool) checkManaged(m Manifest, installed bool) error {
	for _, name := range binaryNames {
		hash, err := digest(filepath.Join(t.Prefix, name))
		if os.IsNotExist(err) {
			if installed {
				return fmt.Errorf("managed binary %s is missing; restore it or use a new --prefix", name)
			}
			continue
		}
		if err != nil {
			return err
		}
		if !installed {
			return fmt.Errorf("%s already exists without an Onibi manifest; choose another --prefix or move that file explicitly", filepath.Join(t.Prefix, name))
		}
		if hash != m.Files[name] {
			return fmt.Errorf("%s changed since installation; refusing to overwrite or remove it", name)
		}
	}
	return nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func (t *Tool) Install(ctx context.Context, update bool, opts LifecycleOptions) error {
	if err := t.prepare(); err != nil {
		return err
	}
	if err := t.prefix(); err != nil {
		return err
	}
	title := "INSTALL"
	if update {
		title = "UPDATE"
	}
	t.UI.Banner(title + " / source checkout")
	m, installed, err := t.manifest()
	if err != nil {
		return err
	}
	if update && !installed {
		return errors.New("no managed installation at this prefix; run make install first")
	}
	if err := t.checkManaged(m, installed); err != nil {
		return err
	}
	t.UI.Field("Source", t.Root)
	t.UI.Field("Destination", t.Prefix)
	if installed {
		t.UI.Field("Installed", m.Version+" / "+m.Commit)
	}
	t.UI.Field("State", "Preserved: browser grants, receipts, configuration and credentials")
	if opts.Pull && !update {
		return errors.New("--pull is only valid with update")
	}
	if opts.RestartWeb && !update {
		return errors.New("--restart-web is only valid with update")
	}
	if opts.DryRun {
		t.UI.Note("Preview only. Would build, verify, and atomically replace managed binaries.")
		if opts.Pull {
			t.UI.Note("Would fetch origin/main and fast-forward only a clean main checkout.")
		}
		if opts.RestartWeb {
			t.UI.Note("Would explicitly restart the web login service after installation.")
		}
		return nil
	}
	if err := os.MkdirAll(t.Prefix, 0755); err != nil {
		return err
	}
	lock, err := lockDirectory(t.Prefix)
	if err != nil {
		return err
	}
	defer lock.Close()
	// Recheck after obtaining the cross-process lock.
	m, installed, err = t.manifest()
	if err != nil {
		return err
	}
	if update && !installed {
		return errors.New("installation changed; inspect again before updating")
	}
	if err := t.checkManaged(m, installed); err != nil {
		return err
	}
	if opts.Pull {
		if err := t.pull(ctx); err != nil {
			return err
		}
	}
	var version, commit string
	err = t.build(ctx, func(builtVersion, builtCommit string) error {
		version, commit = builtVersion, builtCommit
		if err := ctx.Err(); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(t.Prefix, ".install-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		next := Manifest{Schema: 1, Source: t.Root, Version: version, Commit: commit, InstalledAt: time.Now().UTC(), Files: map[string]string{}}
		err = t.UI.Step("Stage verified installation", func(func(string)) error {
			for _, name := range binaryNames {
				path := filepath.Join(stage, name)
				if err := copyExecutable(filepath.Join(t.Root, "bin", name), path); err != nil {
					return err
				}
				hash, err := digest(path)
				if err != nil {
					return err
				}
				next.Files[name] = hash
			}
			raw, err := json.MarshalIndent(next, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(stage, manifestName), append(raw, '\n'), 0600)
		})
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = t.UI.Step("Install binaries and ownership manifest", func(func(string)) error {
			return promote(stage, t.Prefix, append(append([]string{}, binaryNames...), manifestName))
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	t.UI.Field("Installed", version+" / "+commit)
	found := false
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(dir) == t.Prefix {
			found = true
		}
	}
	if !found {
		t.UI.Note("Add this directory to your shell PATH:")
		t.UI.Line("  export PATH=%s:\"$PATH\"", shellQuote(t.Prefix))
	}
	t.UI.Note("Start the cockpit: " + shellQuote(filepath.Join(t.Prefix, "onibi")) + " web")
	if opts.RestartWeb {
		if err := t.UI.Step("Restart web login service (requested)", func(func(string)) error {
			out, err := t.Runner.Run(ctx, t.Root, nil, filepath.Join(t.Prefix, "onibi"), []string{"web", "service", "restart"}, nil)
			if err != nil {
				return fmt.Errorf("binaries were updated, but restart failed: %w: %s", err, strings.TrimSpace(out))
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		t.UI.Note("Running processes keep their current version. Restart them explicitly when ready.")
	}
	return nil
}
func (t *Tool) pull(ctx context.Context) error {
	branch, err := t.capture(ctx, "git", "branch", "--show-current")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != "main" {
		return errors.New("--pull requires main; switch branches explicitly")
	}
	status, err := t.capture(ctx, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("--pull requires a clean checkout, including untracked files; commit or move your work first")
	}
	return t.UI.Step("Fetch and fast-forward origin/main", func(update func(string)) error {
		if _, err := t.Runner.Run(ctx, t.Root, nil, "git", []string{"fetch", "origin", "main"}, update); err != nil {
			return err
		}
		if _, err := t.capture(ctx, "git", "merge-base", "--is-ancestor", "HEAD", "FETCH_HEAD"); err != nil {
			return errors.New("local main diverges from origin/main; refusing to reset or rebase it")
		}
		_, err := t.Runner.Run(ctx, t.Root, nil, "git", []string{"merge", "--ff-only", "FETCH_HEAD"}, update)
		return err
	})
}
func (t *Tool) Status(ctx context.Context) error {
	if err := t.prepare(); err != nil {
		return err
	}
	if err := t.prefix(); err != nil {
		return err
	}
	t.UI.Banner("INSTALLATION / local source build")
	t.UI.Field("Destination", t.Prefix)
	m, installed, err := t.manifest()
	if err != nil {
		return err
	}
	if !installed {
		t.UI.Note("Not installed here. Run make install, or set INSTALL_DIR to the existing installation.")
		return nil
	}
	t.UI.Field("Version", m.Version)
	t.UI.Field("Commit", m.Commit)
	t.UI.Field("Source", m.Source)
	t.UI.Field("Installed", m.InstalledAt.Format(time.RFC3339))
	if err := t.checkManaged(m, true); err != nil {
		return err
	}
	t.UI.Note("Both managed binaries match their installation checksums.")
	return nil
}

type managedService struct {
	path string
	args []string
}

func (t *Tool) services(home string) ([]managedService, error) {
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	var candidates []managedService
	if runtime.GOOS == "darwin" {
		for _, entry := range []struct {
			label string
			args  []string
		}{{"sh.onibi.web", []string{"web", "service", "remove"}}, {"sh.onibi.daemon", []string{"system", "service", "remove"}}} {
			candidates = append(candidates, managedService{filepath.Join(home, "Library", "LaunchAgents", entry.label+".plist"), entry.args})
		}
	} else {
		candidates = append(candidates, managedService{filepath.Join(home, ".config", "systemd", "user", "onibi.service"), []string{"system", "service", "remove"}})
	}
	var owned []managedService
	for _, candidate := range candidates {
		info, err := os.Lstat(candidate.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return nil, fmt.Errorf("inspect service manually: %s", candidate.path)
		}
		raw, err := os.ReadFile(candidate.path)
		if err != nil {
			return nil, err
		}
		exe := filepath.Join(t.Prefix, "onibi")
		match := false
		if runtime.GOOS == "darwin" {
			match = plistExecutable(raw) == exe
		} else {
			match = strings.Contains(string(raw), "ExecStart="+strconv.Quote(exe)+" start")
		}
		if !match {
			return nil, fmt.Errorf("service %s belongs to another installation; remove it explicitly before uninstalling", candidate.path)
		}
		owned = append(owned, candidate)
	}
	return owned, nil
}
func plistExecutable(raw []byte) string {
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	key := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "key" {
			if decoder.DecodeElement(&key, &start) != nil {
				return ""
			}
		} else if start.Name.Local == "array" && key == "ProgramArguments" {
			var args struct {
				Values []string `xml:"string"`
			}
			if decoder.DecodeElement(&args, &start) != nil || len(args.Values) == 0 {
				return ""
			}
			return args.Values[0]
		}
	}
}
func (t *Tool) Uninstall(ctx context.Context, opts LifecycleOptions) error {
	if err := t.prepare(); err != nil {
		return err
	}
	if err := t.prefix(); err != nil {
		return err
	}
	t.UI.Banner("UNINSTALL / preserve your work")
	t.UI.Field("Destination", t.Prefix)
	m, installed, err := t.manifest()
	if err != nil {
		return err
	}
	if !installed {
		t.UI.Note("No managed installation found. No files were removed.")
		return nil
	}
	if err := t.checkManaged(m, true); err != nil {
		return err
	}
	services, err := t.services(opts.Home)
	if err != nil {
		return err
	}
	for _, svc := range services {
		t.UI.Field("Remove job", svc.path)
	}
	for _, name := range binaryNames {
		t.UI.Field("Remove", filepath.Join(t.Prefix, name))
	}
	t.UI.Field("Preserve", "State, grants, credentials, logs, checkout and Ghostty terminals")
	t.UI.Note("Foreground servers are not stopped. Stop them with Ctrl-C before uninstalling.")
	t.UI.Note("Remembered browser access is retained in the state database. Revoke grants before uninstalling if you want to retire that access.")
	if opts.DryRun {
		t.UI.Note("Preview only. Nothing was stopped or removed.")
		return nil
	}
	lock, err := lockDirectory(t.Prefix)
	if err != nil {
		return err
	}
	defer lock.Close()
	m, installed, err = t.manifest()
	if err != nil {
		return err
	}
	if !installed {
		return errors.New("installation changed; inspect again")
	}
	if err := t.checkManaged(m, true); err != nil {
		return err
	}
	services, err = t.services(opts.Home)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if err := t.UI.Step("Stop and remove owned login service", func(func(string)) error {
			out, err := t.Runner.Run(ctx, t.Root, nil, filepath.Join(t.Prefix, "onibi"), svc.args, nil)
			if err != nil {
				return fmt.Errorf("service removal failed; binaries retained: %w: %s", err, strings.TrimSpace(out))
			}
			if _, err := os.Stat(svc.path); !os.IsNotExist(err) {
				return fmt.Errorf("service configuration still exists; binaries retained: %s", svc.path)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Move owned files to a temporary directory before deletion, restoring them
	// if any move fails. Never recursively remove the installation directory.
	stage, err := os.MkdirTemp(t.Prefix, ".remove-")
	if err != nil {
		return err
	}
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = os.RemoveAll(stage)
		}
	}()
	moved := []string{}
	err = t.UI.Step("Remove verified managed binaries", func(func(string)) error {
		for _, name := range append(append([]string{}, binaryNames...), manifestName) {
			if err := os.Rename(filepath.Join(t.Prefix, name), filepath.Join(stage, name)); err != nil {
				var restore error
				for _, old := range moved {
					restore = errors.Join(restore, os.Rename(filepath.Join(stage, old), filepath.Join(t.Prefix, old)))
				}
				if restore != nil {
					keepRecovery = true
					restore = fmt.Errorf("recovery copies retained at %s: %w", stage, restore)
				}
				return errors.Join(err, restore)
			}
			moved = append(moved, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := os.RemoveAll(stage); err != nil {
		keepRecovery = true
		return fmt.Errorf("managed paths removed, but staged files remain at %s: %w", stage, err)
	}
	t.UI.Note("Uninstalled. Preserved data can be reused by a later installation.")
	t.UI.Note("Any separately launched foreground Onibi process must be stopped with Ctrl-C.")
	return nil
}
