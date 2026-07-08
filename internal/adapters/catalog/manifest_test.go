package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/store"
)

func TestParseManifestRejectsHigherMinOnibiVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v0.3.0"
	t.Cleanup(func() { buildinfo.Version = old })
	_, err := ParseManifest([]byte(testManifest("future", "9.0.0")), "future.toml")
	if err == nil || !strings.Contains(err.Error(), "requires Onibi >= 9.0.0, running v0.3.0") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseManifestAcceptsDevMajorVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v2-dev"
	t.Cleanup(func() { buildinfo.Version = old })
	manifest, err := ParseManifest([]byte(testManifest("devok", "1.9.0")), "devok.toml")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "devok" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestManifestAdapterVerifyRunsCommandsAndReportsErrors(t *testing.T) {
	dir := t.TempDir()
	verifyPath := filepath.Join(dir, "verify.out")
	body := testManifestWithHooks("verifyhook", "0.3.0", []string{
		"printf verified > " + strconvQuote(verifyPath),
		"printf nope; exit 12",
	}, nil)
	src := filepath.Join(dir, "verifyhook.toml")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest([]byte(body), src)
	if err != nil {
		t.Fatal(err)
	}
	db := openTestDB(t, dir)
	if err := common.Record(context.Background(), db, manifest.Name, manifest.SourcePath, []byte(body)); err != nil {
		t.Fatal(err)
	}
	err = manifest.Adapter.Verify(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "exit status 12") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("verify err = %v", err)
	}
	if got, err := os.ReadFile(verifyPath); err != nil || strings.TrimSpace(string(got)) != "verified" {
		t.Fatalf("verify command output err=%v body=%q", err, got)
	}
}

func TestManifestAdapterInstallAdoptVerifyRecordsProvenance(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install.out")
	verifyPath := filepath.Join(dir, "verify.out")
	adoptPath := filepath.Join(dir, "adopt.out")
	body := testManifestWithHooks("provenance", "0.3.0",
		[]string{"printf verified > " + strconvQuote(verifyPath)},
		[]string{"printf adopted > " + strconvQuote(adoptPath)},
	)
	installCmd := "printf installed > " + strconvQuote(installPath)
	body = strings.Replace(body, `hook_install = ["printf ok"]`, `hook_install = [`+strconvQuote(installCmd)+`]`, 1)
	src := filepath.Join(dir, "provenance.toml")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest([]byte(body), src)
	if err != nil {
		t.Fatal(err)
	}
	db := openTestDB(t, dir)
	if err := manifest.Adapter.Install(context.Background(), db, "/tmp/onibi-notify"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got, err := os.ReadFile(installPath); err != nil || strings.TrimSpace(string(got)) != "installed" {
		t.Fatalf("install command output err=%v body=%q", err, got)
	}
	assertManifestRecord(t, db, manifest)
	if err := manifest.Adapter.Verify(context.Background(), db); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got, err := os.ReadFile(verifyPath); err != nil || strings.TrimSpace(string(got)) != "verified" {
		t.Fatalf("verify command output err=%v body=%q", err, got)
	}
	if err := common.DeleteRecord(context.Background(), db, manifest.Name, manifest.SourcePath); err != nil {
		t.Fatal(err)
	}
	info := manifest.Adapter.Status(context.Background(), db)
	if !info.Adoptable || info.HashRecorded {
		t.Fatalf("status before adopt = %+v", info)
	}
	if err := manifest.Adapter.Adopt(context.Background(), db); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got, err := os.ReadFile(adoptPath); err != nil || strings.TrimSpace(string(got)) != "adopted" {
		t.Fatalf("adopt command output err=%v body=%q", err, got)
	}
	assertManifestRecord(t, db, manifest)
}

func TestWithRuntimeAdapterRefreshesManifestSourcePath(t *testing.T) {
	dir := t.TempDir()
	body := testManifestWithHooks("refreshpath", "0.3.0", nil, nil)
	src := filepath.Join(dir, "src.toml")
	dest := filepath.Join(dir, "dest.toml")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest([]byte(body), src)
	if err != nil {
		t.Fatal(err)
	}
	manifest.SourcePath = dest
	manifest = WithRuntimeAdapter(manifest)
	db := openTestDB(t, dir)
	if err := manifest.Adapter.Install(context.Background(), db, "/tmp/onibi-notify"); err != nil {
		t.Fatal(err)
	}
	info := manifest.Adapter.Status(context.Background(), db)
	if info.InstallPath != dest {
		t.Fatalf("install path = %q want %q", info.InstallPath, dest)
	}
	if _, ok, err := common.RecordFor(context.Background(), db, manifest.Name, dest); err != nil || !ok {
		t.Fatalf("dest provenance ok=%v err=%v", ok, err)
	}
	if _, ok, err := common.RecordFor(context.Background(), db, manifest.Name, src); err != nil || ok {
		t.Fatalf("src provenance ok=%v err=%v", ok, err)
	}
}

func TestManifestStatusHidesMissingSourcePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "missing.toml")
	body := testManifestWithHooks("missingpath", "0.3.0", nil, nil)
	manifest, err := ParseManifest([]byte(body), src)
	if err != nil {
		t.Fatal(err)
	}
	info := manifest.Adapter.Status(context.Background(), openTestDB(t, dir))
	if info.Installed || info.Managed || info.InstallPath != "" || strings.Contains(info.Message, src) {
		t.Fatalf("status leaked missing source: %+v", info)
	}
}

func testManifest(name, minVersion string) string {
	return strings.Join([]string{
		`name = "` + name + `"`,
		`version = "1.0.0"`,
		`kind = "agent"`,
		`cmd_pattern = { PreToolUse = "*" }`,
		`hook_install = ["printf ok"]`,
		`hook_uninstall = ["printf ok"]`,
		`min_onibi_version = "` + minVersion + `"`,
		``,
		`[risk_overrides]`,
		`Write = "high"`,
		``,
	}, "\n")
}

func testManifestWithHooks(name, minVersion string, verify, adopt []string) string {
	lines := []string{
		`name = "` + name + `"`,
		`version = "1.0.0"`,
		`kind = "agent"`,
		`cmd_pattern = { PreToolUse = "*" }`,
		`hook_install = ["printf ok"]`,
		`hook_uninstall = ["printf ok"]`,
	}
	if len(verify) > 0 {
		lines = append(lines, `hook_verify = [`+quoteList(verify)+`]`)
	}
	if len(adopt) > 0 {
		lines = append(lines, `hook_adopt = [`+quoteList(adopt)+`]`)
	}
	lines = append(lines,
		`min_onibi_version = "`+minVersion+`"`,
		``,
		`[risk_overrides]`,
		`Write = "high"`,
		``,
	)
	return strings.Join(lines, "\n")
}

func openTestDB(t *testing.T, dir string) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertManifestRecord(t *testing.T, db *store.DB, manifest Manifest) {
	t.Helper()
	if _, ok, err := common.RecordFor(context.Background(), db, manifest.Name, manifest.SourcePath); err != nil || !ok {
		t.Fatalf("manifest record ok=%v err=%v", ok, err)
	}
}

func quoteList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconvQuote(value))
	}
	return strings.Join(quoted, ", ")
}

func strconvQuote(value string) string {
	return strconv.Quote(value)
}
