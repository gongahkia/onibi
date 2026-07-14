package ssh

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"

	"github.com/gongahkia/onibi/internal/buildinfo"
)

const (
	onibiBinary       = "onibi"
	notifyBinary      = "onibi-notify"
	defaultRemoteBins = "$HOME/.local/bin"
)

type InstallOptions struct {
	Version      string
	LocalBinDir  string
	ReleaseRoot  string
	RemoteBinDir string
	Rand         io.Reader
}

type LocalBinaries struct {
	Dir    string
	Onibi  string
	Notify string
}

type InstallResult struct {
	SourceDir    string
	RemoteBinDir string
	TempDir      string
}

func (c *Client) InstallBinaries(platform Platform, opts InstallOptions) (InstallResult, error) {
	if err := ValidatePlatform(platform); err != nil {
		return InstallResult{}, err
	}
	binaries, err := ResolveLocalBinaries(platform, opts)
	if err != nil {
		return InstallResult{}, err
	}
	token, err := randomToken(opts.Rand)
	if err != nil {
		return InstallResult{}, err
	}
	tempDir := "/tmp/onibi." + token + "." + platform.ArtifactSuffix()
	sc, err := sftp.NewClient(c.Client)
	if err != nil {
		return InstallResult{}, err
	}
	defer sc.Close()
	if err := sc.MkdirAll(tempDir); err != nil {
		return InstallResult{}, err
	}
	onibiSHA256, err := uploadExecutable(sc, binaries.Onibi, tempDir+"/"+onibiBinary)
	if err != nil {
		return InstallResult{}, err
	}
	notifySHA256, err := uploadExecutable(sc, binaries.Notify, tempDir+"/"+notifyBinary)
	if err != nil {
		return InstallResult{}, err
	}
	cmd := installCommand(tempDir, remoteBinDir(opts.RemoteBinDir), token, onibiSHA256, notifySHA256)
	if err := c.runRemote(cmd); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{SourceDir: binaries.Dir, RemoteBinDir: remoteBinDir(opts.RemoteBinDir), TempDir: tempDir}, nil
}

func ResolveLocalBinaries(platform Platform, opts InstallOptions) (LocalBinaries, error) {
	if err := ValidatePlatform(platform); err != nil {
		return LocalBinaries{}, err
	}
	for _, dir := range localSourceDirs(platform, opts) {
		onibi := filepath.Join(dir, onibiBinary)
		notify := filepath.Join(dir, notifyBinary)
		if executableFile(onibi) && executableFile(notify) {
			return LocalBinaries{Dir: dir, Onibi: onibi, Notify: notify}, nil
		}
	}
	return LocalBinaries{}, fmt.Errorf("ssh: matching binaries not found for %s", platform.ArtifactSuffix())
}

func localSourceDirs(platform Platform, opts InstallOptions) []string {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = buildinfo.Version
	}
	binDir := strings.TrimSpace(opts.LocalBinDir)
	if binDir == "" {
		binDir = "bin"
	}
	releaseRoot := strings.TrimSpace(opts.ReleaseRoot)
	if releaseRoot == "" {
		if home, err := os.UserHomeDir(); err == nil {
			releaseRoot = filepath.Join(home, ".local", "share", "onibi", "release")
		}
	}
	dirs := []string{binDir}
	if releaseRoot != "" {
		dirs = append(dirs, filepath.Join(releaseRoot, version, platform.ReleaseDirName()))
		dirs = append(dirs, filepath.Join(releaseRoot, version, platform.ArtifactSuffix()))
	}
	return dirs
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func uploadExecutable(sc *sftp.Client, local, remote string) (string, error) {
	src, err := os.Open(local)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := sc.Create(remote)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, hash), src); err != nil {
		_ = dst.Close()
		return "", err
	}
	if err := dst.Close(); err != nil {
		return "", err
	}
	if err := sc.Chmod(remote, 0755); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (c *Client) runRemote(cmd string) error {
	_, err := c.RunOutput(cmd)
	return err
}

func (c *Client) RunOutput(cmd string) (string, error) {
	session, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("ssh: remote command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func installCommand(tempDir, remoteDir, token, onibiSHA256, notifySHA256 string) string {
	binDir := remoteDirExpr(remoteDir)
	onibiTmp := ".onibi." + token
	notifyTmp := ".onibi-notify." + token
	return strings.Join([]string{
		"set -eu",
		"sha256() {",
		`  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'`,
		`  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'`,
		`  else echo "ssh: remote SHA-256 utility unavailable" >&2; exit 1; fi`,
		"}",
		`test "$(sha256 ` + shellQuote(tempDir+"/"+onibiBinary) + `)" = ` + shellQuote(onibiSHA256) + ` || { echo "ssh: artifact checksum mismatch: onibi" >&2; exit 1; }`,
		`test "$(sha256 ` + shellQuote(tempDir+"/"+notifyBinary) + `)" = ` + shellQuote(notifySHA256) + ` || { echo "ssh: artifact checksum mismatch: onibi-notify" >&2; exit 1; }`,
		"mkdir -p " + binDir,
		"install -m 0755 " + shellQuote(tempDir+"/"+onibiBinary) + " " + binDir + "/" + onibiTmp,
		"install -m 0755 " + shellQuote(tempDir+"/"+notifyBinary) + " " + binDir + "/" + notifyTmp,
		"mv -f " + binDir + "/" + onibiTmp + " " + binDir + "/" + onibiBinary,
		"mv -f " + binDir + "/" + notifyTmp + " " + binDir + "/" + notifyBinary,
		"rm -rf " + shellQuote(tempDir),
	}, "\n")
}

func remoteBinDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return defaultRemoteBins
	}
	return dir
}

func remoteDirExpr(dir string) string {
	if dir == "" || dir == defaultRemoteBins || dir == "~/.local/bin" {
		return `"$HOME/.local/bin"`
	}
	return shellQuote(dir)
}

func randomToken(r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	raw := make([]byte, 8)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("/._:-", r))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func ValidatePlatform(platform Platform) error {
	switch platform {
	case Platform{GOOS: "linux", GOARCH: "arm64"},
		Platform{GOOS: "linux", GOARCH: "arm", GOARM: "7"},
		Platform{GOOS: "linux", GOARCH: "arm", GOARM: "6"},
		Platform{GOOS: "linux", GOARCH: "amd64"},
		Platform{GOOS: "darwin", GOARCH: "arm64"},
		Platform{GOOS: "darwin", GOARCH: "amd64"}:
		return nil
	default:
		return errors.New("ssh: unsupported platform")
	}
}
