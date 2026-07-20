package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/catalog"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/spf13/cobra"
)

type adapterStatus struct {
	Name             string                    `json:"name"`
	Support          string                    `json:"support"`
	Certified        bool                      `json:"certified"`
	Contract         *adapters.AdapterContract `json:"contract,omitempty"`
	Detected         bool                      `json:"detected"`
	Installed        bool                      `json:"installed"`
	Managed          bool                      `json:"managed"`
	HashRecorded     bool                      `json:"hash_recorded"`
	Tampered         bool                      `json:"tampered"`
	Adoptable        bool                      `json:"adoptable"`
	InstalledVersion string                    `json:"installed_version,omitempty"`
	BundledVersion   string                    `json:"bundled_version"`
	Outdated         bool                      `json:"outdated"`
	Path             string                    `json:"path,omitempty"`
	Message          string                    `json:"message,omitempty"`
	Next             string                    `json:"next,omitempty"`
}

func runAdapters(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	if err := adapters.LoadExternalManifests(); err != nil {
		return err
	}
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	var rows []adapterStatus
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(cmd.Context(), db)
		rows = append(rows, statusFromInfo(info, agentDetected(name)))
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "NAME", "SUPPORT", "V1", "DETECTED", "INSTALLED", "VERSION", "PATH", "STATUS")}
	for _, r := range rows {
		ver := r.InstalledVersion
		if ver == "" {
			ver = "-"
		}
		if r.Outdated {
			ver = style.yellow(ver + " (outdated)")
		}
		table = append(table, []string{
			r.Name,
			r.Support,
			style.bool(r.Certified),
			style.bool(r.Detected),
			style.installed(r.Installed),
			ver,
			r.Path,
			adapterMessage(style, r),
		})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func adaptersAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path-or-url>",
		Short: "Add a third-party adapter manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdaptersAdd,
	}
	cmd.Flags().String("sha256", "", "required SHA-256 pin for HTTPS manifest URLs")
	return cmd
}

func adaptersValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a third-party adapter manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdaptersValidate,
	}
}

func runAdaptersAdd(cmd *cobra.Command, args []string) error {
	pin, _ := cmd.Flags().GetString("sha256")
	src := args[0]
	body, sourcePath, err := readAdapterManifestSource(cmd.Context(), src, pin)
	if err != nil {
		return err
	}
	manifest, err := catalog.ParseManifest(body, sourcePath)
	if err != nil {
		return err
	}
	if err := adapters.LoadExternalManifests(); err != nil {
		return err
	}
	if _, err := adapters.ManifestFor(manifest.Name); err == nil {
		return fmt.Errorf("adapter %q already registered", manifest.Name)
	}
	destDir, err := adapters.ExternalManifestDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(destDir, manifest.Name+".toml")
	if err := os.WriteFile(dest, body, 0o600); err != nil {
		return err
	}
	manifest.SourcePath = dest
	if err := adapters.Register(manifest); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added adapter %s -> %s\n", manifest.Name, dest)
	return nil
}

func runAdaptersValidate(cmd *cobra.Command, args []string) error {
	path := args[0]
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	manifest, err := catalog.ParseManifest(body, abs)
	if err != nil {
		line := adapterManifestErrorLine(body, err)
		fmt.Fprintf(cmd.ErrOrStderr(), "%s:%d: %v\n", abs, line, err)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "OK %s %s\n", manifest.Name, manifest.Version)
	return nil
}

func readAdapterManifestSource(ctx context.Context, src, pin string) ([]byte, string, error) {
	u, err := url.Parse(src)
	if err == nil && u.Scheme != "" {
		if u.Scheme != "https" {
			return nil, "", errors.New("adapter manifest URLs must use https")
		}
		if strings.TrimSpace(pin) == "" {
			return nil, "", errors.New("--sha256 is required for adapter manifest URLs")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("fetch adapter manifest: %s", resp.Status)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
		if err != nil {
			return nil, "", err
		}
		if len(body) > 1<<20 {
			return nil, "", errors.New("adapter manifest too large")
		}
		if err := verifyAdapterManifestSHA256(body, pin); err != nil {
			return nil, "", err
		}
		return body, src, nil
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return nil, "", err
	}
	if len(body) > 1<<20 {
		return nil, "", errors.New("adapter manifest too large")
	}
	if strings.TrimSpace(pin) != "" {
		if err := verifyAdapterManifestSHA256(body, pin); err != nil {
			return nil, "", err
		}
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return nil, "", err
	}
	return body, abs, nil
}

func verifyAdapterManifestSHA256(body []byte, want string) error {
	want = strings.TrimSpace(strings.ToLower(want))
	if len(want) != sha256.Size*2 {
		return errors.New("sha256 pin must be 64 hex chars")
	}
	if _, err := hex.DecodeString(want); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("sha256 mismatch: got %s", got)
	}
	return nil
}

func adapterManifestErrorLine(body []byte, err error) int {
	msg := err.Error()
	if match := regexp.MustCompile(`(?i)line\s+([0-9]+)`).FindStringSubmatch(msg); len(match) == 2 {
		if n, convErr := strconv.Atoi(match[1]); convErr == nil && n > 0 {
			return n
		}
	}
	for key, needle := range map[string]string{
		"name":              "name",
		"version":           "version",
		"kind":              "kind",
		"cmd_pattern":       "cmd_pattern",
		"hook_install":      "hook_install",
		"hook_uninstall":    "hook_uninstall",
		"risk_overrides":    "risk",
		"min_onibi_version": "min_onibi_version",
	} {
		if strings.Contains(msg, needle) {
			if line := lineForManifestKey(body, key); line > 0 {
				return line
			}
		}
	}
	return 1
}

func lineForManifestKey(body []byte, key string) int {
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, "["+key+"]") {
			return i + 1
		}
	}
	return 1
}

func statusFromInfo(info common.Info, detected bool) adapterStatus {
	installed := ""
	if info.InstalledVersion != nil {
		installed = *info.InstalledVersion
	}
	row := adapterStatus{
		Name:             info.Name,
		Support:          info.Support,
		Detected:         detected,
		Installed:        info.Installed,
		Managed:          info.Managed,
		HashRecorded:     info.HashRecorded,
		Tampered:         info.Tampered,
		Adoptable:        info.Adoptable,
		InstalledVersion: installed,
		BundledVersion:   info.BundledVersion,
		Outdated:         info.Outdated,
		Path:             info.InstallPath,
		Message:          info.Message,
		Next:             info.Next,
	}
	if contract, ok := adapters.ContractFor(info.Name); ok {
		row.Certified = true
		row.Contract = &contract
	}
	return row
}

func agentDetected(name string) bool {
	bin := map[string]string{
		"amp":      "amp",
		"claude":   "claude",
		"codex":    "codex",
		"copilot":  "copilot",
		"gemini":   "gemini",
		"goose":    "goose",
		"opencode": "opencode",
		"pi":       "pi",
	}[name]
	if bin == "" {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func adapterMessage(style cliStyle, r adapterStatus) string {
	if r.Tampered {
		return style.red(r.Message)
	}
	if r.Installed && !r.Outdated {
		return style.green(r.Message)
	}
	if r.Installed && r.Outdated {
		return style.yellow(r.Message)
	}
	if r.Next != "" {
		return style.yellow(r.Message)
	}
	return style.dim(r.Message)
}
