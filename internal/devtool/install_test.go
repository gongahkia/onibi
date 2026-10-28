package devtool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestInstallUpdatePreviewAndOwnedUninstall(t *testing.T) {
	tool, r, _ := sourceTool(t)
	tool.Prefix = filepath.Join(t.TempDir(), "bin with spaces")
	opts := LifecycleOptions{Home: t.TempDir(), DryRun: true}
	if err := tool.Install(t.Context(), false, opts); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Fatal("preview ran external command")
	}
	if _, err := os.Stat(tool.Prefix); !os.IsNotExist(err) {
		t.Fatal("preview created prefix")
	}
	opts.DryRun = false
	if err := tool.Install(t.Context(), false, opts); err != nil {
		t.Fatal(err)
	}
	manifest, ok, err := tool.manifest()
	if err != nil || !ok {
		t.Fatal("missing manifest", err)
	}
	if err := tool.checkManaged(manifest, true); err != nil {
		t.Fatal(err)
	}
	r.contents = "next version"
	r.failCompile = true
	if err := tool.Install(t.Context(), true, opts); err == nil {
		t.Fatal("failed update succeeded")
	}
	if raw, _ := os.ReadFile(filepath.Join(tool.Prefix, "onibi")); string(raw) != "new binary" {
		t.Fatal("failed update lost installed executable")
	}
	r.failCompile = false
	if err := tool.Install(t.Context(), true, opts); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(tool.Prefix, "keep-me")
	os.WriteFile(unrelated, []byte("preserve"), 0600)
	opts.DryRun = true
	if err := tool.Uninstall(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tool.Prefix, "onibi")); err != nil {
		t.Fatal("uninstall preview removed binary")
	}
	os.WriteFile(filepath.Join(tool.Prefix, "onibi"), []byte("modified by owner"), 0755)
	opts.DryRun = false
	if err := tool.Uninstall(t.Context(), opts); err == nil {
		t.Fatal("removed modified binary")
	}
	os.WriteFile(filepath.Join(tool.Prefix, "onibi"), []byte("next version"), 0755)
	if err := tool.Uninstall(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(binaryNames, manifestName) {
		if _, err := os.Stat(filepath.Join(tool.Prefix, name)); !os.IsNotExist(err) {
			t.Fatalf("managed file remains: %s", name)
		}
	}
	if raw, _ := os.ReadFile(unrelated); string(raw) != "preserve" {
		t.Fatal("uninstall touched unrelated file")
	}
}
func TestRetiredNotifierFromOlderInstallIsRemoved(t *testing.T) {
	legacy := func(t *testing.T, notifier string) (*Tool, LifecycleOptions) {
		t.Helper()
		tool, _, _ := sourceTool(t)
		tool.Prefix = t.TempDir()
		opts := LifecycleOptions{Home: t.TempDir()}
		if err := tool.Install(t.Context(), false, opts); err != nil {
			t.Fatal(err)
		}
		m, ok, err := tool.manifest()
		if err != nil || !ok {
			t.Fatal("missing manifest", err)
		}
		path := filepath.Join(tool.Prefix, "onibi-notify")
		if err := os.WriteFile(path, []byte("old notifier"), 0755); err != nil {
			t.Fatal(err)
		}
		hash, err := digest(path)
		if err != nil {
			t.Fatal(err)
		}
		m.Files["onibi-notify"] = hash
		raw, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(tool.Prefix, manifestName), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if notifier != "old notifier" {
			if err := os.WriteFile(path, []byte(notifier), 0755); err != nil {
				t.Fatal(err)
			}
		}
		return tool, opts
	}
	t.Run("update", func(t *testing.T) {
		tool, opts := legacy(t, "old notifier")
		if err := tool.Install(t.Context(), true, opts); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(tool.Prefix, "onibi-notify")); !os.IsNotExist(err) {
			t.Fatal("update kept the retired notifier")
		}
		m, ok, err := tool.manifest()
		if err != nil || !ok || len(m.Files) != len(binaryNames) {
			t.Fatalf("manifest=%#v ok=%v err=%v", m, ok, err)
		}
		if err := tool.Status(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("uninstall", func(t *testing.T) {
		tool, opts := legacy(t, "old notifier")
		if err := tool.Uninstall(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"onibi", "onibi-notify", manifestName} {
			if _, err := os.Stat(filepath.Join(tool.Prefix, name)); !os.IsNotExist(err) {
				t.Fatalf("managed file remains: %s", name)
			}
		}
	})
	t.Run("modified notifier is kept", func(t *testing.T) {
		tool, opts := legacy(t, "edited by owner")
		if err := tool.Install(t.Context(), true, opts); err == nil {
			t.Fatal("updated over a modified notifier")
		}
		if err := tool.Uninstall(t.Context(), opts); err == nil {
			t.Fatal("uninstalled over a modified notifier")
		}
		if raw, _ := os.ReadFile(filepath.Join(tool.Prefix, "onibi-notify")); string(raw) != "edited by owner" {
			t.Fatal("modified notifier was touched")
		}
	})
	t.Run("deleted notifier is ignored", func(t *testing.T) {
		tool, opts := legacy(t, "old notifier")
		if err := os.Remove(filepath.Join(tool.Prefix, "onibi-notify")); err != nil {
			t.Fatal(err)
		}
		if err := tool.Install(t.Context(), true, opts); err != nil {
			t.Fatal(err)
		}
	})
}
func TestManifestRejectsUnknownFiles(t *testing.T) {
	tool, _, _ := sourceTool(t)
	tool.Prefix = t.TempDir()
	raw, _ := json.Marshal(Manifest{Schema: 1, Files: map[string]string{"onibi": strings.Repeat("a", 64), "other": strings.Repeat("b", 64)}})
	if err := os.WriteFile(filepath.Join(tool.Prefix, manifestName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tool.manifest(); err == nil {
		t.Fatal("accepted a manifest owning an unknown file")
	}
}
func TestInstallRefusesUnmanagedFileAndBuildDirectory(t *testing.T) {
	tool, _, _ := sourceTool(t)
	tool.Prefix = t.TempDir()
	os.WriteFile(filepath.Join(tool.Prefix, "onibi"), []byte("unmanaged"), 0755)
	if err := tool.Install(t.Context(), false, LifecycleOptions{}); err == nil {
		t.Fatal("overwrote unmanaged binary")
	}
	tool.Prefix = filepath.Join(tool.Root, "bin")
	if err := tool.Install(t.Context(), false, LifecycleOptions{}); err == nil {
		t.Fatal("accepted build output as install prefix")
	}
}

type serviceFailRunner struct{ *fakeRunner }

func (r serviceFailRunner) Run(ctx context.Context, dir string, env []string, name string, args []string, line func(string)) (string, error) {
	if len(args) == 3 && args[1] == "service" && args[2] == "remove" {
		return "permission denied", errors.New("stop failed")
	}
	return r.fakeRunner.Run(ctx, dir, env, name, args, line)
}
func TestUninstallServiceFailureRetainsBinaries(t *testing.T) {
	tool, r, _ := sourceTool(t)
	tool.Prefix = t.TempDir()
	home := t.TempDir()
	opts := LifecycleOptions{Home: home}
	if err := tool.Install(t.Context(), false, opts); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(tool.Prefix, "onibi")
	var path, body string
	if runtime.GOOS == "darwin" {
		path = filepath.Join(home, "Library", "LaunchAgents", "sh.onibi.web.plist")
		body = `<plist><dict><key>ProgramArguments</key><array><string>` + exe + `</string><string>web</string></array></dict></plist>`
	} else {
		path = filepath.Join(home, ".config", "systemd", "user", "onibi.service")
		body = "[Service]\nExecStart=" + strconv.Quote(exe) + " start\n"
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(body), 0600)
	tool.Runner = serviceFailRunner{r}
	if err := tool.Uninstall(t.Context(), opts); err == nil || !strings.Contains(err.Error(), "binaries retained") {
		t.Fatalf("ignored failed service stop: %v", err)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatal("removed executable after failed stop")
	}
	os.WriteFile(path, []byte(strings.ReplaceAll(body, exe, "/another/install/onibi")), 0600)
	if err := tool.Uninstall(t.Context(), opts); err == nil || !strings.Contains(err.Error(), "another installation") {
		t.Fatal("removed unrelated service", err)
	}
}

func TestSourceUninstallRecognizesLinuxWebUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux service ownership")
	}
	tool, _, _ := sourceTool(t)
	tool.Prefix = filepath.Join(t.TempDir(), "tools %n $PATH")
	home := t.TempDir()
	exe := filepath.Join(tool.Prefix, "onibi")
	path := filepath.Join(home, ".config", "systemd", "user", "onibi-web.service")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	body := "[Service]\nExecStart=:" + strconv.Quote(strings.ReplaceAll(exe, "%", "%%")) + " web --background --listen localhost:8787\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	services, err := tool.services(home)
	if err != nil || len(services) != 1 || strings.Join(services[0].args, " ") != "web service remove" {
		t.Fatalf("Linux web unit unrecognized: %v %#v", err, services)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(body, "ExecStart=", "# ExecStart=")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.services(home); err == nil {
		t.Fatal("comment mistaken for service ownership")
	}
}

type pullRunner struct {
	branch, status string
	diverged       bool
	calls          []string
}

func (r *pullRunner) Run(_ context.Context, _ string, _ []string, name string, args []string, _ func(string)) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	switch args[0] {
	case "branch":
		return r.branch, nil
	case "status":
		return r.status, nil
	case "merge-base":
		if r.diverged {
			return "", errors.New("not ancestor")
		}
	}
	return "", nil
}
func TestPullOnlyFastForwardsCleanMain(t *testing.T) {
	for _, tt := range []struct {
		branch, status    string
		diverged, success bool
	}{{"feature", "", false, false}, {"main", " M work.go", false, false}, {"main", "", true, false}, {"main", "", false, true}} {
		tool, _, _ := sourceTool(t)
		r := &pullRunner{branch: tt.branch, status: tt.status, diverged: tt.diverged}
		tool.Runner = r
		err := tool.pull(t.Context())
		if (err == nil) != tt.success {
			t.Fatalf("unexpected result: %v", err)
		}
		calls := strings.Join(r.calls, "\n")
		if strings.Contains(calls, "reset") || strings.Contains(calls, "rebase") || strings.Contains(calls, "checkout") {
			t.Fatal("destructive source update")
		}
		if strings.Contains(calls, "git merge --ff-only") != tt.success {
			t.Fatal("incorrect merge", calls)
		}
	}
}
