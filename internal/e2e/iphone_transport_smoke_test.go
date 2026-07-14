package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIPhoneTransportSmokeRedactsPairURL(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	out := t.TempDir()
	pairURL := "https://192.168.1.42:8443/pair/not-recorded#k=also-not-recorded"
	args := []string{
		"--transport=lan",
		"--pair-url=" + pairURL,
		"--out=" + out,
		"--skip-local-checks",
		"--allow-skip",
		"--record=setup_health=pass",
		"--record=pairing=pass",
		"--record=approval=pass",
		"--record=intervention=pass",
		"--record=reconnect=pass",
		"--record=teardown=pass",
		"--record=failure_diagnostics=pass",
	}
	cmd := exec.Command(filepath.Join(root, "scripts", "iphone-transport-smoke.sh"), args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runner: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
	path := filepath.Join(out, entries[0].Name())
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%#o err=%v", info.Mode().Perm(), err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "not-recorded") || strings.Contains(string(body), "also-not-recorded") {
		t.Fatalf("pair URL leaked: %s", body)
	}
	var artifact struct {
		Schema            string `json:"schema"`
		Transport         string `json:"transport"`
		Origin            string `json:"origin"`
		LocalVerification string `json:"local_verification"`
		Complete          bool   `json:"complete"`
	}
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Schema != "onibi.iphone-transport-smoke.v1" || artifact.Transport != "lan" || artifact.Origin != "https://192.168.1.42:8443" || artifact.LocalVerification != "skip" || artifact.Complete {
		t.Fatalf("artifact=%#v", artifact)
	}
}
