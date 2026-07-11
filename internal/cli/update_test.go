package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

func TestUpdateCheckOnlyStablePrintsLatest(t *testing.T) {
	withFakeUpdateAPI(t)
	out, _ := executeRoot(t, "update", "--check-only", "--channel", "stable", "--color", "never")
	got := out.String()
	if !strings.Contains(got, "latest stable: v1.2.3") || !strings.Contains(got, "url: http://") {
		t.Fatalf("output = %q", got)
	}
}

func TestUpdateCheckOnlyBetaPrintsLatestPrerelease(t *testing.T) {
	withFakeUpdateAPI(t)
	out, _ := executeRoot(t, "update", "--check-only", "--channel", "beta", "--color", "never")
	got := out.String()
	if !strings.Contains(got, "latest beta: v1.3.0-beta.1") || !strings.Contains(got, "https://example.test/v1.3.0-beta.1") {
		t.Fatalf("output = %q", got)
	}
}

func TestUpdateCheckJSONIncludesSchemaVersion(t *testing.T) {
	withUpdateTempHome(t)
	old := updateCheckRun
	updateCheckRun = func(context.Context, updatecheck.Options) updatecheck.Result {
		return updatecheck.Result{
			Status:         updatecheck.StatusCurrent,
			Source:         updatecheck.SourceGitHub,
			CurrentVersion: "v1.2.3",
			CurrentCommit:  "abc123",
			LatestVersion:  "v1.2.3",
			URL:            "https://example.test/releases/v1.2.3",
			Detail:         "current v1.2.3 is up to date with latest release v1.2.3",
		}
	}
	t.Cleanup(func() { updateCheckRun = old })
	out, _ := executeRoot(t, "update-check", "--json", "--no-github", "--color", "never")
	var got updatecheck.Result
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("parse json: %v\n%s", err, out.String())
	}
	if got.SchemaVersion != updatecheck.SchemaVersion {
		t.Fatalf("schema_version = %q", got.SchemaVersion)
	}
	if got.CurrentVersion != "v1.2.3" || got.LatestVersion != "v1.2.3" || got.Status != updatecheck.StatusCurrent {
		t.Fatalf("result = %#v", got)
	}
}

func TestUpdateApplyGuardUntilVerificationExists(t *testing.T) {
	withUpdateTempHome(t)
	withFakeUpdateAPI(t)
	out, errOut, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "release public key not configured") {
		t.Fatalf("err = %v stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
}

func TestUpdateApplyVerifiesAndAppliesBinary(t *testing.T) {
	withUpdateTempHome(t)
	binary := []byte("#!/bin/sh\necho onibi\n")
	archive := testUpdateArchive(t, map[string][]byte{"onibi": binary})
	asset := updateAssetName("v1.2.3")
	checksums := testUpdateChecksums(asset, archive)
	pub, sig := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, sig)
	oldKey := updateReleasePublicKey
	oldApply := updateApply
	oldCodeSign := updateCodeSignVerify
	oldRestart := updateRestartService
	oldExec := updateExec
	var applied []byte
	var oldSavePath string
	var execPath string
	var verified bool
	updateReleasePublicKey = pub
	updateApply = func(next []byte, previous string) error {
		applied = append([]byte(nil), next...)
		oldSavePath = previous
		return nil
	}
	updateCodeSignVerify = func(_ context.Context, path string) error {
		verified = true
		if b, err := osReadFile(path); err != nil || !bytes.Equal(b, binary) {
			t.Fatalf("codesign path bytes = %q err=%v", b, err)
		}
		return nil
	}
	updateRestartService = func(context.Context, config.Paths, string) (bool, error) {
		return false, nil
	}
	updateExec = func(path string, _ []string, _ []string) error {
		execPath = path
		return nil
	}
	t.Cleanup(func() {
		updateReleasePublicKey = oldKey
		updateApply = oldApply
		updateCodeSignVerify = oldCodeSign
		updateRestartService = oldRestart
		updateExec = oldExec
	})
	out, _ := executeRoot(t, "update", "--channel", "stable", "--color", "never")
	if !strings.Contains(out.String(), "updated to v1.2.3") {
		t.Fatalf("output = %q", out.String())
	}
	if !bytes.Equal(applied, binary) || !verified {
		t.Fatalf("applied=%q verified=%v", applied, verified)
	}
	if filepath.Base(oldSavePath) != "onibi.prev" {
		t.Fatalf("old save path = %q", oldSavePath)
	}
	if execPath == "" {
		t.Fatal("updated binary was not re-execed")
	}
}

func TestActivateUpdatedBinaryRestartsInstalledService(t *testing.T) {
	target := filepath.Join(t.TempDir(), "onibi")
	paths := config.Paths{StateDir: t.TempDir(), LogDir: t.TempDir()}
	oldExecutable := updateExecutablePath
	oldRestart := updateRestartService
	oldExec := updateExec
	var restarted bool
	updateExecutablePath = func() (string, error) { return target, nil }
	updateRestartService = func(_ context.Context, gotPaths config.Paths, executable string) (bool, error) {
		if gotPaths.StateDir != paths.StateDir || executable != target {
			t.Fatalf("restart args paths=%#v executable=%q", gotPaths, executable)
		}
		restarted = true
		return true, nil
	}
	updateExec = func(string, []string, []string) error {
		t.Fatal("exec should not run when service restarts")
		return nil
	}
	t.Cleanup(func() {
		updateExecutablePath = oldExecutable
		updateRestartService = oldRestart
		updateExec = oldExec
	})
	if err := activateUpdatedBinary(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if !restarted {
		t.Fatal("service was not restarted")
	}
}

func TestActivateUpdatedBinaryExecsWithoutService(t *testing.T) {
	target := filepath.Join(t.TempDir(), "onibi")
	paths := config.Paths{StateDir: t.TempDir(), LogDir: t.TempDir()}
	oldExecutable := updateExecutablePath
	oldRestart := updateRestartService
	oldExec := updateExec
	var execPath string
	updateExecutablePath = func() (string, error) { return target, nil }
	updateRestartService = func(context.Context, config.Paths, string) (bool, error) {
		return false, nil
	}
	updateExec = func(path string, _ []string, _ []string) error {
		execPath = path
		return nil
	}
	t.Cleanup(func() {
		updateExecutablePath = oldExecutable
		updateRestartService = oldRestart
		updateExec = oldExec
	})
	if err := activateUpdatedBinary(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if execPath != target {
		t.Fatalf("exec path = %q", execPath)
	}
}

func TestUpdateRollbackSwapsPreviousBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(t.TempDir(), "onibi")
	if err := os.WriteFile(current, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := updatePreviousPath(paths)
	if err := os.WriteFile(previous, []byte("previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutable := updateExecutablePath
	updateExecutablePath = func() (string, error) { return current, nil }
	t.Cleanup(func() { updateExecutablePath = oldExecutable })
	out, _ := executeRoot(t, "update", "--rollback", "--color", "never")
	if !strings.Contains(out.String(), "rolled back") {
		t.Fatalf("output = %q", out.String())
	}
	if got, err := os.ReadFile(current); err != nil || string(got) != "previous" {
		t.Fatalf("current = %q err=%v", got, err)
	}
	if got, err := os.ReadFile(previous); err != nil || string(got) != "current" {
		t.Fatalf("previous = %q err=%v", got, err)
	}
}

func TestUpdateChecksumMismatchAborts(t *testing.T) {
	withUpdateTempHome(t)
	archive := testUpdateArchive(t, map[string][]byte{"onibi": []byte("binary")})
	checksums := []byte(strings.Repeat("0", 64) + "  " + updateAssetName("v1.2.3") + "\n")
	pub, sig := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, sig)
	withUpdatePublicKey(t, pub)
	_, _, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateBadSignatureAborts(t *testing.T) {
	withUpdateTempHome(t)
	archive := testUpdateArchive(t, map[string][]byte{"onibi": []byte("binary")})
	checksums := testUpdateChecksums(updateAssetName("v1.2.3"), archive)
	pub, _ := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, []byte("bad signature"))
	withUpdatePublicKey(t, pub)
	_, _, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "verify checksums signature") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateUsesEmbeddedReleasePublicKey(t *testing.T) {
	oldOverride := updateReleasePublicKey
	oldEmbedded := buildinfo.ReleasePublicKeyB64
	updateReleasePublicKey = ""
	t.Cleanup(func() {
		updateReleasePublicKey = oldOverride
		buildinfo.ReleasePublicKeyB64 = oldEmbedded
	})
	checksums := []byte("abc")
	pub, sig := testUpdateSignature(t, checksums)
	buildinfo.ReleasePublicKeyB64 = base64.StdEncoding.EncodeToString([]byte(pub))
	if err := verifyUpdateSignature(checksums, sig); err != nil {
		t.Fatalf("verifyUpdateSignature: %v", err)
	}
}

func TestUpdateUnsafeArchivePathAborts(t *testing.T) {
	withUpdateTempHome(t)
	archive := testUpdateArchive(t, map[string][]byte{"../onibi": []byte("binary")})
	checksums := testUpdateChecksums(updateAssetName("v1.2.3"), archive)
	pub, sig := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, sig)
	withUpdatePublicKey(t, pub)
	_, _, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "unsafe archive path") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateCodeSignFailureAborts(t *testing.T) {
	withUpdateTempHome(t)
	archive := testUpdateArchive(t, map[string][]byte{"onibi": []byte("binary")})
	checksums := testUpdateChecksums(updateAssetName("v1.2.3"), archive)
	pub, sig := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, sig)
	withUpdatePublicKey(t, pub)
	oldCodeSign := updateCodeSignVerify
	updateCodeSignVerify = func(context.Context, string) error { return errors.New("codesign failed") }
	t.Cleanup(func() { updateCodeSignVerify = oldCodeSign })
	_, _, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "codesign failed") {
		t.Fatalf("err = %v", err)
	}
}

func withFakeUpdateAPI(t *testing.T) {
	asset := updateAssetName("v1.2.3")
	archive := testUpdateArchive(t, map[string][]byte{"onibi": []byte("binary")})
	checksums := testUpdateChecksums(asset, archive)
	withFakeUpdateAssets(t, archive, checksums, []byte("signature"))
}

func withFakeUpdateAssets(t *testing.T, archive, checksums, signature []byte) {
	t.Helper()
	oldLatest := updateLatestURL
	oldReleases := updateReleasesURL
	oldClient := updateHTTPClient
	assetName := updateAssetName("v1.2.3")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
				t.Fatalf("headers = %#v", r.Header)
			}
			fmt.Fprintf(w, `{"tag_name":"v1.2.3","html_url":"%s/releases/tag/v1.2.3","assets":[{"name":%q,"browser_download_url":"%s/%s"},{"name":"checksums.txt","browser_download_url":"%s/checksums.txt"},{"name":"checksums.txt.sig","browser_download_url":"%s/checksums.txt.sig"}]}`, server.URL, assetName, server.URL, assetName, server.URL, server.URL)
		case "/releases":
			if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
				t.Fatalf("headers = %#v", r.Header)
			}
			if r.URL.Query().Get("per_page") != "1" {
				t.Fatalf("query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.3.0-beta.1","html_url":"https://example.test/v1.3.0-beta.1","prerelease":true}]`))
		case "/" + assetName:
			_, _ = w.Write(archive)
		case "/checksums.txt":
			_, _ = w.Write(checksums)
		case "/checksums.txt.sig":
			_, _ = w.Write(signature)
		default:
			http.NotFound(w, r)
		}
	}))
	updateLatestURL = server.URL + "/latest"
	updateReleasesURL = server.URL + "/releases?per_page=1"
	updateHTTPClient = server.Client()
	t.Cleanup(func() {
		updateLatestURL = oldLatest
		updateReleasesURL = oldReleases
		updateHTTPClient = oldClient
		server.Close()
	})
}

func withUpdatePublicKey(t *testing.T, key string) {
	t.Helper()
	old := updateReleasePublicKey
	updateReleasePublicKey = key
	t.Cleanup(func() { updateReleasePublicKey = old })
}

func withUpdateTempHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
}

var osReadFile = func(name string) ([]byte, error) { return os.ReadFile(name) }

func testUpdateArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testUpdateChecksums(asset string, archive []byte) []byte {
	sum := sha256.Sum256(archive)
	return []byte(fmt.Sprintf("%x  %s\n", sum[:], asset))
}

func testUpdateSignature(t *testing.T, checksums []byte) (string, []byte) {
	t.Helper()
	entity, err := openpgp.NewEntity("Onibi Test", "", "test@example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	var pub bytes.Buffer
	armored, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := entity.Serialize(armored); err != nil {
		t.Fatal(err)
	}
	if err := armored.Close(); err != nil {
		t.Fatal(err)
	}
	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, entity, bytes.NewReader(checksums), nil); err != nil {
		t.Fatal(err)
	}
	return pub.String(), sig.Bytes()
}
