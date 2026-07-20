package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
)

func TestAdapterStatusReportsCertifiedContract(t *testing.T) {
	row := statusFromInfo(common.Info{Name: "codex"}, false)
	if !row.Certified || row.Contract == nil || row.Contract.Agent != "codex" || !row.Contract.Approval.BlocksTool || !row.Contract.Approval.ReviewRequired {
		t.Fatalf("certified row=%+v", row)
	}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	contract, ok := got["contract"].(map[string]any)
	approval, approvalOK := contract["approval"].(map[string]any)
	if !ok || !approvalOK || got["certified"] != true || contract["agent"] != "codex" || contract["version"] != "1" || approval["review_required"] != true {
		t.Fatalf("json=%s", body)
	}
	for _, name := range []string{"opencode", "gemini"} {
		deferred := statusFromInfo(common.Info{Name: name}, false)
		if deferred.Certified || deferred.Contract != nil {
			t.Fatalf("deferred %s row=%+v", name, deferred)
		}
	}
}

func TestAdaptersAddLocalManifestCopiesAndRegisters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ONIBI_ADAPTERS_DIR", filepath.Join(dir, "adapters"))
	src := filepath.Join(dir, "aider.toml")
	body := testAdapterManifest("aiderlocal", filepath.Join(dir, "install.out"))
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "adapters", "add", src, "--color", "never")
	if !strings.Contains(out.String(), "Added adapter aiderlocal") {
		t.Fatalf("out = %q", out.String())
	}
	dest := filepath.Join(dir, "adapters", "aiderlocal.toml")
	if got, err := os.ReadFile(dest); err != nil || string(got) != body {
		t.Fatalf("copied manifest err=%v body=%q", err, got)
	}
	if manifest, err := adapters.ManifestFor("aiderlocal"); err != nil || manifest.Name != "aiderlocal" {
		t.Fatalf("manifest = %#v err=%v", manifest, err)
	}
}

func TestAdaptersAddHTTPSManifestRequiresSHA256Pin(t *testing.T) {
	_, _, err := executeRootAllowError(t, "adapters", "add", "https://example.com/aider.toml", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "--sha256 is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdaptersAddHTTPSManifestWithSHA256Pin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ONIBI_ADAPTERS_DIR", filepath.Join(dir, "adapters"))
	body := testAdapterManifest("aiderurl", filepath.Join(dir, "install.out"))
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()
	oldClient := http.DefaultClient
	http.DefaultClient = ts.Client()
	t.Cleanup(func() { http.DefaultClient = oldClient })
	sum := sha256.Sum256([]byte(body))
	out, _ := executeRoot(t, "adapters", "add", ts.URL, "--sha256", hex.EncodeToString(sum[:]), "--color", "never")
	if !strings.Contains(out.String(), "Added adapter aiderurl") {
		t.Fatalf("out = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "adapters", "aiderurl.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptersAddManifestRoutesInstallHooks(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	adapterDir := filepath.Join(home, ".config", "onibi", "adapters")
	t.Setenv("ONIBI_ADAPTERS_DIR", adapterDir)
	installed := filepath.Join(home, "installed.txt")
	src := filepath.Join(home, "aider.toml")
	if err := os.WriteFile(src, []byte(testAdapterManifest("aiderhooks", installed)), 0o600); err != nil {
		t.Fatal(err)
	}
	executeRoot(t, "adapters", "add", src, "--color", "never")
	executeRoot(t, "install-hooks", "--agent", "aiderhooks", "--color", "never")
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "installed" {
		t.Fatalf("installed = %q", got)
	}
}

func TestAdaptersValidateManifestReportsOK(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "aider.toml")
	if err := os.WriteFile(src, []byte(testAdapterManifest("aidervalid", filepath.Join(dir, "install.out"))), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := executeRoot(t, "adapters", "validate", src, "--color", "never")
	if !strings.Contains(out.String(), "OK aidervalid 1.0.0") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestAdaptersValidateManifestReportsLineNumber(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.toml")
	body := strings.Join([]string{
		`name = "badvalidate"`,
		`version = "1.0.0"`,
		`kind = "agent"`,
		`cmd_pattern = { PreToolUse = "*" }`,
		`hook_install = ["printf ok"]`,
		`hook_uninstall = ["printf ok"]`,
		`min_onibi_version = "0.3.0"`,
		``,
		`[risk_overrides]`,
		`Write = "severe"`,
		``,
	}, "\n")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := executeRootAllowError(t, "adapters", "validate", src, "--color", "never")
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(errOut.String(), "bad.toml:9:") || !strings.Contains(errOut.String(), "invalid risk level") {
		t.Fatalf("stderr = %q err=%v", errOut.String(), err)
	}
}

func TestExampleAiderAdapterIsNonCertifiedPostEditOnly(t *testing.T) {
	home, _, _ := hooksCLIFixture(t)
	t.Setenv("ONIBI_ADAPTERS_DIR", filepath.Join(home, ".config", "onibi", "adapters"))
	path := filepath.Join("..", "..", "examples", "aider-adapter", "aider.toml")
	executeRoot(t, "adapters", "validate", path, "--color", "never")
	executeRoot(t, "adapters", "add", path, "--color", "never")
	if _, ok := adapters.ContractFor("aider"); ok {
		t.Fatal("aider example has a certified contract")
	}
	executeRoot(t, "install-hooks", "--agent", "aider", "--color", "never")
	for _, want := range []string{
		filepath.Join(home, ".config", "onibi", "aider", "aider.onibi.conf.yml"),
		filepath.Join(home, ".local", "bin", "onibi-aider-event"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "onibi-aider-approval")); !os.IsNotExist(err) {
		t.Fatalf("unexpected approval helper: %v", err)
	}
}

func testAdapterManifest(name, installPath string) string {
	installCmd := "printf installed > " + strconv.Quote(installPath)
	uninstallCmd := "printf uninstalled > " + strconv.Quote(installPath)
	return strings.Join([]string{
		`name = "` + name + `"`,
		`version = "1.0.0"`,
		`kind = "agent"`,
		`cmd_pattern = { PreToolUse = "*" }`,
		`hook_install = [` + strconv.Quote(installCmd) + `]`,
		`hook_uninstall = [` + strconv.Quote(uninstallCmd) + `]`,
		`min_onibi_version = "0.3.0"`,
		``,
		`[risk_overrides]`,
		`Write = "high"`,
		``,
	}, "\n")
}
