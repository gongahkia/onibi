package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
func GitRepository(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is optional")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	for k, v := range map[string]string{"user.name": "Shack tests", "user.email": "tests@example.invalid", "commit.gpgsign": "false", "core.hooksPath": filepath.Join(dir, "no-hooks"), "core.attributesFile": "/dev/null", "core.fsmonitor": "false", "merge.conflictStyle": "diff3"} {
		Git(t, dir, "config", k, v)
	}
	return dir
}
func ConflictedRepository(t *testing.T) string {
	t.Helper()
	dir := GitRepository(t)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "conflict file.txt"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("header\nbase α\nfooter\n")
	Git(t, dir, "add", "--", "conflict file.txt")
	Git(t, dir, "commit", "-qm", "base")
	Git(t, dir, "checkout", "-qb", "other")
	write("header\ntheirs 🦦\nfooter\n")
	Git(t, dir, "commit", "-qam", "theirs")
	Git(t, dir, "checkout", "-q", "main")
	write("header\nours 😀\nfooter\n")
	Git(t, dir, "commit", "-qam", "ours")
	if err := exec.Command("git", "-C", dir, "merge", "--no-edit", "other").Run(); err == nil {
		t.Fatal("fixture did not conflict")
	}
	return dir
}

func DeletionConflictRepository(t *testing.T) string {
	t.Helper()
	dir := GitRepository(t)
	path := filepath.Join(dir, "conflict file.txt")
	if err := os.WriteFile(path, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "add", "--", "conflict file.txt")
	Git(t, dir, "commit", "-qm", "base")
	Git(t, dir, "checkout", "-qb", "other")
	Git(t, dir, "rm", "-q", "--", "conflict file.txt")
	Git(t, dir, "commit", "-qm", "delete")
	Git(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(path, []byte("ours modified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "commit", "-qam", "modify")
	if err := exec.Command("git", "-C", dir, "merge", "--no-edit", "other").Run(); err == nil {
		t.Fatal("fixture did not conflict")
	}
	return dir
}
