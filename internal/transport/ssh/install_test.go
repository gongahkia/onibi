package ssh

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocalBinariesPrefersBinDir(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	release := filepath.Join(root, "release")
	writeExecutable(t, filepath.Join(bin, onibiBinary))
	writeExecutable(t, filepath.Join(bin, notifyBinary))
	writeExecutable(t, filepath.Join(release, "v1", "linux_armv7", onibiBinary))
	writeExecutable(t, filepath.Join(release, "v1", "linux_armv7", notifyBinary))
	got, err := ResolveLocalBinaries(Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"}, InstallOptions{
		Version:     "v1",
		LocalBinDir: bin,
		ReleaseRoot: release,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != bin {
		t.Fatalf("dir = %q, want %q", got.Dir, bin)
	}
}

func TestResolveLocalBinariesUsesReleaseDir(t *testing.T) {
	root := t.TempDir()
	releaseDir := filepath.Join(root, "release", "v1", "linux-armv6")
	writeExecutable(t, filepath.Join(releaseDir, onibiBinary))
	writeExecutable(t, filepath.Join(releaseDir, notifyBinary))
	got, err := ResolveLocalBinaries(Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"}, InstallOptions{
		Version:     "v1",
		LocalBinDir: filepath.Join(root, "missing-bin"),
		ReleaseRoot: filepath.Join(root, "release"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != releaseDir {
		t.Fatalf("dir = %q, want %q", got.Dir, releaseDir)
	}
}

func TestResolveLocalBinariesRequiresExecutables(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	writeFile(t, filepath.Join(bin, onibiBinary), 0644)
	writeExecutable(t, filepath.Join(bin, notifyBinary))
	if _, err := ResolveLocalBinaries(Platform{GOOS: "linux", GOARCH: "amd64"}, InstallOptions{LocalBinDir: bin}); err == nil {
		t.Fatal("expected missing executable error")
	}
}

func TestInstallCommandVerifiesArtifactsBeforeAtomicMove(t *testing.T) {
	cmd := installCommand("/tmp/onibi.abc.linux_armv7", defaultRemoteBins, "abc", strings.Repeat("a", 64), strings.Repeat("b", 64))
	for _, want := range []string{
		`sha256() {`,
		`sha256sum "$1"`,
		`artifact checksum mismatch: onibi`,
		`artifact checksum mismatch: onibi-notify`,
		`mkdir -p "$HOME/.local/bin"`,
		`install -m 0755 /tmp/onibi.abc.linux_armv7/onibi "$HOME/.local/bin"/.onibi.abc`,
		`mv -f "$HOME/.local/bin"/.onibi.abc "$HOME/.local/bin"/onibi`,
		`mv -f "$HOME/.local/bin"/.onibi-notify.abc "$HOME/.local/bin"/onibi-notify`,
		`rm -rf /tmp/onibi.abc.linux_armv7`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}
	if strings.Index(cmd, "artifact checksum mismatch: onibi") > strings.Index(cmd, `mkdir -p "$HOME/.local/bin"`) {
		t.Fatalf("checksum verification must precede install:\n%s", cmd)
	}
}

func TestInstallCommandRejectsChecksumMismatchBeforeReplacement(t *testing.T) {
	root := t.TempDir()
	tempDir := filepath.Join(root, "upload")
	remoteDir := filepath.Join(root, "bin")
	writeExecutable(t, filepath.Join(tempDir, onibiBinary))
	writeExecutable(t, filepath.Join(tempDir, notifyBinary))
	cmd := exec.Command("sh", "-c", installCommand(tempDir, remoteDir, "abc", strings.Repeat("0", 64), strings.Repeat("0", 64)))
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "artifact checksum mismatch: onibi") {
		t.Fatalf("err=%v output=%q", err, out)
	}
	if _, err := os.Stat(filepath.Join(remoteDir, onibiBinary)); !os.IsNotExist(err) {
		t.Fatalf("onibi was replaced after checksum mismatch: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/tmp/onibi.armv7"); got != "/tmp/onibi.armv7" {
		t.Fatalf("quote = %q", got)
	}
	if got := shellQuote("/tmp/onibi it's"); got != `'/tmp/onibi it'"'"'s'` {
		t.Fatalf("quote = %q", got)
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	writeFile(t, path, 0755)
}

func writeFile(t *testing.T, path string, mode uint32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bin"), os.FileMode(mode)); err != nil {
		t.Fatal(err)
	}
}
