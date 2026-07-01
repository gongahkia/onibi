package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
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

func TestUpdateApplyGuardUntilVerificationExists(t *testing.T) {
	withFakeUpdateAPI(t)
	out, errOut, err := executeRootAllowError(t, "update", "--channel", "stable", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "release public key not configured") {
		t.Fatalf("err = %v stdout=%q stderr=%q", err, out.String(), errOut.String())
	}
}

func TestUpdateApplyVerifiesAndAppliesBinary(t *testing.T) {
	binary := []byte("#!/bin/sh\necho onibi\n")
	archive := testUpdateArchive(t, map[string][]byte{"onibi": binary})
	asset := updateAssetName("v1.2.3")
	checksums := testUpdateChecksums(asset, archive)
	pub, sig := testUpdateSignature(t, checksums)
	withFakeUpdateAssets(t, archive, checksums, sig)
	oldKey := updateReleasePublicKey
	oldApply := updateApply
	oldCodeSign := updateCodeSignVerify
	var applied []byte
	var verified bool
	updateReleasePublicKey = pub
	updateApply = func(next []byte) error {
		applied = append([]byte(nil), next...)
		return nil
	}
	updateCodeSignVerify = func(_ context.Context, path string) error {
		verified = true
		if b, err := osReadFile(path); err != nil || !bytes.Equal(b, binary) {
			t.Fatalf("codesign path bytes = %q err=%v", b, err)
		}
		return nil
	}
	t.Cleanup(func() {
		updateReleasePublicKey = oldKey
		updateApply = oldApply
		updateCodeSignVerify = oldCodeSign
	})
	out, _ := executeRoot(t, "update", "--channel", "stable", "--color", "never")
	if !strings.Contains(out.String(), "updated to v1.2.3") {
		t.Fatalf("output = %q", out.String())
	}
	if !bytes.Equal(applied, binary) || !verified {
		t.Fatalf("applied=%q verified=%v", applied, verified)
	}
}

func TestUpdateChecksumMismatchAborts(t *testing.T) {
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

func TestUpdateUnsafeArchivePathAborts(t *testing.T) {
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
