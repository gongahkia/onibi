package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/openpgp"
)

const updateTimeout = 10 * time.Second
const maxUpdateArchiveBytes = 200 << 20
const maxUpdateMetaBytes = 4 << 20

var (
	updateLatestURL        = "https://api.github.com/repos/gongahkia/onibi/releases/latest"
	updateReleasesURL      = "https://api.github.com/repos/gongahkia/onibi/releases?per_page=1"
	updateHTTPClient       = http.DefaultClient
	updateReleasePublicKey = ``
	updateApply            = func(binary []byte, oldSavePath string) error {
		return selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{OldSavePath: oldSavePath})
	}
	updateCodeSignVerify = defaultUpdateCodeSignVerify
	updateDefaultPaths   = config.DefaultPaths
	updateExecutablePath = os.Executable
	updateRestartService = restartUpdatedService
	updateExec           = syscall.Exec
)

type updateRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	channel, _ := cmd.Flags().GetString("channel")
	checkOnly, _ := cmd.Flags().GetBool("check-only")
	rollback, _ := cmd.Flags().GetBool("rollback")
	if rollback {
		if checkOnly {
			return errors.New("--rollback cannot be combined with --check-only")
		}
		return rollbackUpdate(cmd)
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		channel = "stable"
	}
	if channel != "stable" && channel != "beta" {
		return errors.New("--channel must be stable or beta")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), updateTimeout)
	defer cancel()
	rel, err := fetchUpdateRelease(ctx, channel)
	if err != nil {
		return err
	}
	if checkOnly {
		fmt.Fprintf(cmd.OutOrStdout(), "latest %s: %s\n", channel, rel.TagName)
		if rel.HTMLURL != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "url: %s\n", rel.HTMLURL)
		}
		return nil
	}
	paths, err := updateDefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	if err := applyUpdateRelease(ctx, rel, updatePreviousPath(paths)); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "updated to %s\n", rel.TagName)
	return activateUpdatedBinary(ctx, paths)
}

func fetchUpdateRelease(ctx context.Context, channel string) (updateRelease, error) {
	switch channel {
	case "stable":
		return fetchStableRelease(ctx)
	case "beta":
		return fetchBetaRelease(ctx)
	default:
		return updateRelease{}, errors.New("unknown update channel")
	}
}

func fetchStableRelease(ctx context.Context) (updateRelease, error) {
	var rel updateRelease
	if err := getUpdateJSON(ctx, updateLatestURL, &rel); err != nil {
		return updateRelease{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return updateRelease{}, errors.New("stable release missing tag_name")
	}
	return rel, nil
}

func fetchBetaRelease(ctx context.Context) (updateRelease, error) {
	var releases []updateRelease
	if err := getUpdateJSON(ctx, updateReleasesURL, &releases); err != nil {
		return updateRelease{}, err
	}
	for _, rel := range releases {
		if rel.Prerelease && strings.TrimSpace(rel.TagName) != "" {
			return rel, nil
		}
	}
	return updateRelease{}, errors.New("no beta prerelease found")
}

func getUpdateJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "onibi-update")
	if token := updateGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch release: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse release: %w", err)
	}
	return nil
}

func applyUpdateRelease(ctx context.Context, rel updateRelease, oldSavePath string) error {
	asset := updateAssetName(rel.TagName)
	if asset == "" {
		return errors.New("release tag is empty")
	}
	archiveURL := updateAssetURL(rel, asset)
	checksumsURL := updateAssetURL(rel, "checksums.txt")
	signatureURL := updateAssetURL(rel, "checksums.txt.sig")
	checksums, err := getUpdateBytes(ctx, checksumsURL, maxUpdateMetaBytes)
	if err != nil {
		return err
	}
	signature, err := getUpdateBytes(ctx, signatureURL, maxUpdateMetaBytes)
	if err != nil {
		return err
	}
	if err := verifyUpdateSignature(checksums, signature); err != nil {
		return err
	}
	archiveBytes, err := getUpdateBytes(ctx, archiveURL, maxUpdateArchiveBytes)
	if err != nil {
		return err
	}
	if err := verifyUpdateChecksum(asset, archiveBytes, checksums); err != nil {
		return err
	}
	binary, err := extractOnibiBinary(archiveBytes)
	if err != nil {
		return err
	}
	if err := verifyUpdateBinary(ctx, binary); err != nil {
		return err
	}
	return updateApply(binary, oldSavePath)
}

func rollbackUpdate(cmd *cobra.Command) error {
	paths, err := updateDefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	target, err := updateExecutablePath()
	if err != nil {
		return err
	}
	previous := updatePreviousPath(paths)
	if err := rollbackUpdateBinary(target, previous); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "rolled back %s\n", target)
	return nil
}

func activateUpdatedBinary(ctx context.Context, paths config.Paths) error {
	target, err := updateExecutablePath()
	if err != nil {
		return err
	}
	restarted, err := updateRestartService(ctx, paths, target)
	if err != nil {
		return err
	}
	if restarted || runtime.GOOS == "windows" {
		return nil
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil
	}
	return updateExec(target, os.Args, os.Environ())
}

func restartUpdatedService(ctx context.Context, paths config.Paths, executable string) (bool, error) {
	m, err := service.NewManager(paths, executable)
	if err != nil {
		return false, err
	}
	if !m.Status(ctx).Installed {
		return false, nil
	}
	if err := m.Restart(ctx); err != nil {
		return true, err
	}
	return true, nil
}

func updatePreviousPath(paths config.Paths) string {
	return filepath.Join(paths.StateDir, "onibi.prev")
}

func rollbackUpdateBinary(targetPath, previousPath string) error {
	if _, err := os.Stat(previousPath); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("rollback binary not found: %s", previousPath)
	} else if err != nil {
		return err
	}
	currentSavePath := previousPath + ".current"
	_ = os.Remove(currentSavePath)
	if err := moveUpdateFile(targetPath, currentSavePath); err != nil {
		return fmt.Errorf("save current binary: %w", err)
	}
	if err := moveUpdateFile(previousPath, targetPath); err != nil {
		_ = moveUpdateFile(currentSavePath, targetPath)
		return fmt.Errorf("restore previous binary: %w", err)
	}
	if err := moveUpdateFile(currentSavePath, previousPath); err != nil {
		return fmt.Errorf("retain replaced binary: %w", err)
	}
	return nil
}

func moveUpdateFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if err := copyUpdateFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyUpdateFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(dst, info.Mode().Perm())
}

func getUpdateBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "onibi-update")
	if token := updateGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", filepath.Base(req.URL.Path), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", filepath.Base(req.URL.Path), resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", filepath.Base(req.URL.Path), err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("download %s: exceeds %d bytes", filepath.Base(req.URL.Path), limit)
	}
	return body, nil
}

func verifyUpdateSignature(checksums, signature []byte) error {
	key := strings.TrimSpace(updateReleasePublicKey)
	if key == "" {
		return errors.New("release public key not configured")
	}
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key))
	if err != nil {
		return fmt.Errorf("parse release public key: %w", err)
	}
	if _, err := openpgp.CheckDetachedSignature(ring, bytes.NewReader(checksums), bytes.NewReader(signature)); err != nil {
		return fmt.Errorf("verify checksums signature: %w", err)
	}
	return nil
}

func verifyUpdateChecksum(asset string, archiveBytes, checksums []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[len(fields)-1] == asset {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt missing %s", asset)
	}
	gotBytes := sha256.Sum256(archiveBytes)
	got := fmt.Sprintf("%x", gotBytes[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

func extractOnibiBinary(archiveBytes []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		name := strings.TrimSpace(header.Name)
		clean := path.Clean(name)
		if name == "" || path.IsAbs(name) || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
			return nil, fmt.Errorf("unsafe archive path %q", header.Name)
		}
		if clean != "onibi" {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, errors.New("archive onibi entry is not a regular file")
		}
		binary, err := io.ReadAll(io.LimitReader(tr, maxUpdateArchiveBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read onibi binary: %w", err)
		}
		if len(binary) == 0 {
			return nil, errors.New("archive onibi binary is empty")
		}
		if len(binary) > maxUpdateArchiveBytes {
			return nil, errors.New("archive onibi binary exceeds limit")
		}
		return binary, nil
	}
	return nil, errors.New("archive missing onibi binary")
}

func verifyUpdateBinary(ctx context.Context, binary []byte) error {
	tmp, err := os.CreateTemp("", "onibi-update-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return err
	}
	return updateCodeSignVerify(ctx, name)
}

func defaultUpdateCodeSignVerify(ctx context.Context, binaryPath string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.CommandContext(ctx, "codesign", "--verify", binaryPath).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("codesign verify: %s", msg)
	}
	return nil
}

func updateAssetName(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	version := strings.TrimPrefix(tag, "v")
	return fmt.Sprintf("onibi_%s_%s_%s.tar.gz", version, runtime.GOOS, updateArchiveArch(runtime.GOARCH))
}

func updateArchiveArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "i386"
	default:
		return goarch
	}
}

func updateAssetURL(rel updateRelease, name string) string {
	for _, asset := range rel.Assets {
		if asset.Name == name && strings.TrimSpace(asset.BrowserDownloadURL) != "" {
			return strings.TrimSpace(asset.BrowserDownloadURL)
		}
	}
	base := strings.TrimRight(rel.HTMLURL, "/")
	tag := strings.TrimSpace(rel.TagName)
	if strings.Contains(base, "/releases/tag/") {
		base = strings.TrimSuffix(base[:strings.LastIndex(base, "/releases/tag/")], "/") + "/releases/download/" + tag
	} else if base == "" {
		base = "https://github.com/gongahkia/onibi/releases/download/" + tag
	}
	return strings.TrimRight(base, "/") + "/" + name
}

func updateGitHubToken() string {
	if token := strings.TrimSpace(os.Getenv("ONIBI_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}
