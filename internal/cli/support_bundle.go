package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/miniappurl"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

type supportBundle struct {
	GeneratedAt        time.Time                 `json:"generated_at"`
	Redacted           bool                      `json:"redacted"`
	Paths              map[string]string         `json:"paths"`
	Version            supportVersion            `json:"version"`
	System             supportSystem             `json:"system"`
	Doctor             doctor.Report             `json:"doctor"`
	HookMatrix         []hooksMatrixRow          `json:"hook_matrix"`
	HookReports        []hooksShowReport         `json:"hook_reports"`
	Config             []supportConfigKey        `json:"config"`
	EncryptedReadiness supportEncryptedReadiness `json:"encrypted_readiness"`
	Database           supportDatabase           `json:"database"`
	Audit              []supportAuditEntry       `json:"audit"`
	Logs               map[string][]string       `json:"logs"`
	Errors             []string                  `json:"errors,omitempty"`
}

type supportVersion struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type supportSystem struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	Shell  string `json:"shell,omitempty"`
	Term   string `json:"term,omitempty"`
}

type supportConfigKey struct {
	Key      string `json:"key"`
	Current  string `json:"current"`
	Default  string `json:"default"`
	Explicit bool   `json:"explicit"`
}

type supportEncryptedReadiness struct {
	Mode                 string `json:"mode"`
	SeedPresent          bool   `json:"seed_present"`
	SeedError            string `json:"seed_error,omitempty"`
	MiniAppURLSet        bool   `json:"mini_app_url_set"`
	MiniAppURLAllowed    bool   `json:"mini_app_url_allowed"`
	WebAppActionLastSeen string `json:"webapp_action_last_seen"`
}

type supportDatabase struct {
	SchemaVersion int    `json:"schema_version"`
	Error         string `json:"error,omitempty"`
}

type supportAuditEntry struct {
	ID            int64     `json:"id"`
	TS            time.Time `json:"ts"`
	Action        string    `json:"action"`
	SessionID     string    `json:"session_id,omitempty"`
	PayloadHash   string    `json:"payload_hash,omitempty"`
	DecidedByChat string    `json:"decided_by_chat,omitempty"`
	Detail        string    `json:"detail,omitempty"`
}

func supportBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "support-bundle",
		Short: "Print redacted support bundle JSON",
		RunE:  runSupportBundle,
	}
	cmd.Flags().Bool("redacted", false, "required; redact secrets and local paths")
	cmd.Flags().Bool("include-chat-id", false, "include Telegram chat ids in audit rows")
	return cmd
}

func runSupportBundle(cmd *cobra.Command, _ []string) error {
	redacted, _ := cmd.Flags().GetBool("redacted")
	includeChatID, _ := cmd.Flags().GetBool("include-chat-id")
	if !redacted {
		return errors.New("--redacted required")
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		return err
	}
	defer db.Close()
	bundle := buildSupportBundle(cmd, paths, db, includeChatID)
	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	body = append(redactSupportBundle(body, homeDir()), '\n')
	_, err = cmd.OutOrStdout().Write(body)
	return err
}

func buildSupportBundle(cmd *cobra.Command, paths config.Paths, db *store.DB, includeChatID bool) supportBundle {
	ctx := cmd.Context()
	cfg, meta, cfgErr := config.Load(paths)
	errs := []string{}
	if cfgErr != nil {
		errs = append(errs, "config: "+cfgErr.Error())
		cfg = config.Default()
		meta = config.LoadMeta{Path: paths.Config, Explicit: map[string]bool{}}
	}
	matrix, err := buildHooksMatrix(cmd, db, hooksShowNotifyBin())
	if err != nil {
		errs = append(errs, "hook matrix: "+err.Error())
	}
	reports, reportErrs := supportHookReports(cmd, db, hooksShowNotifyBin())
	errs = append(errs, reportErrs...)
	doctorReport := doctor.Run(ctx, doctor.Options{Paths: paths, Offline: true})
	logs, logErrs := supportLogs(paths)
	errs = append(errs, logErrs...)
	return supportBundle{
		GeneratedAt:        time.Now(),
		Redacted:           true,
		Paths:              supportPaths(paths),
		Version:            supportVersion{Version: buildinfo.Version, Commit: buildinfo.Commit, Date: buildinfo.Date},
		System:             supportSystem{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Shell: os.Getenv("SHELL"), Term: os.Getenv("TERM")},
		Doctor:             doctorReport,
		HookMatrix:         matrix,
		HookReports:        reports,
		Config:             supportConfig(cfg, meta),
		EncryptedReadiness: supportEncrypted(ctx, paths, cfg, db),
		Database:           supportDatabaseVersion(ctx, db),
		Audit:              supportAudit(ctx, db, includeChatID),
		Logs:               logs,
		Errors:             errs,
	}
}

func supportPaths(paths config.Paths) map[string]string {
	return map[string]string{
		"state":  paths.StateDir,
		"config": paths.Config,
		"db":     paths.DBFile,
		"logs":   paths.LogDir,
		"socket": paths.Socket,
	}
}

func supportHookReports(cmd *cobra.Command, db *store.DB, notifyBin string) ([]hooksShowReport, []string) {
	var reports []hooksShowReport
	var errs []string
	for _, name := range adapters.Names() {
		report, err := buildHooksShowReport(cmd, db, name, notifyBin)
		if err != nil {
			errs = append(errs, "hook "+name+": "+err.Error())
			continue
		}
		reports = append(reports, report)
	}
	return reports, errs
}

func supportConfig(cfg config.Config, meta config.LoadMeta) []supportConfigKey {
	rows := config.Keys(cfg, meta)
	out := make([]supportConfigKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, supportConfigKey{
			Key:      row.Key,
			Current:  redactConfigScalar(row.Key, row.Current),
			Default:  redactConfigScalar(row.Key, row.Default),
			Explicit: row.Explicit,
		})
	}
	return out
}

func redactConfigScalar(key, value string) string {
	low := strings.ToLower(key)
	if strings.Contains(low, "token") || strings.Contains(low, "secret") || strings.Contains(low, "seed") || strings.Contains(low, "password") || strings.Contains(low, "api_key") {
		return "[REDACTED]"
	}
	return value
}

func supportEncrypted(ctx context.Context, paths config.Paths, cfg config.Config, db *store.DB) supportEncryptedReadiness {
	out := supportEncryptedReadiness{
		Mode:              strings.TrimSpace(cfg.Telegram.EncryptedMode),
		MiniAppURLSet:     strings.TrimSpace(cfg.Telegram.MiniAppURL) != "",
		MiniAppURLAllowed: miniappurl.Allowed(cfg.Telegram.MiniAppURL),
	}
	if out.Mode == "" {
		out.Mode = "off"
	}
	sec, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		out.SeedError = err.Error()
	} else {
		seed, ok, err := sec.GetWithTimeout(ctx, secrets.KeyEnvelopeSeed, 2*time.Second)
		if err != nil {
			out.SeedError = err.Error()
		}
		out.SeedPresent = ok && strings.TrimSpace(seed) != ""
	}
	out.WebAppActionLastSeen = "never"
	if v, ok, err := db.KVGetString(ctx, "secure:last_webapp_action"); err == nil && ok {
		out.WebAppActionLastSeen = v
	}
	return out
}

func supportDatabaseVersion(ctx context.Context, db *store.DB) supportDatabase {
	out := supportDatabase{}
	err := db.SQL().QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&out.SchemaVersion)
	if err != nil && err != sql.ErrNoRows {
		out.Error = err.Error()
	}
	return out
}

func supportAudit(ctx context.Context, db *store.DB, includeChatID bool) []supportAuditEntry {
	rows, err := db.AuditRecent(ctx, 50)
	if err != nil {
		return nil
	}
	out := make([]supportAuditEntry, 0, len(rows))
	for _, row := range rows {
		chat := ""
		if row.DecidedByChat != 0 {
			if includeChatID {
				chat = strconv.FormatInt(row.DecidedByChat, 10)
			} else {
				chat = "[REDACTED]"
			}
		}
		out = append(out, supportAuditEntry{
			ID:            row.ID,
			TS:            row.TS,
			Action:        row.Action,
			SessionID:     row.SessionID,
			PayloadHash:   row.PayloadHash,
			DecidedByChat: chat,
			Detail:        safeAuditDetail(row),
		})
	}
	return out
}

func safeAuditDetail(row store.AuditEntry) string {
	switch {
	case strings.HasPrefix(row.Action, "prompt."),
		strings.HasPrefix(row.Action, "approval."),
		strings.Contains(row.Action, "session_input"):
		if row.Detail == "" {
			return ""
		}
		return "[REDACTED]"
	default:
		return row.Detail
	}
}

func supportLogs(paths config.Paths) (map[string][]string, []string) {
	names := []string{"onibi.log", "onibi.out.log", "onibi.err.log"}
	out := map[string][]string{}
	var errs []string
	for _, name := range names {
		path := filepath.Join(paths.LogDir, name)
		lines, err := tailLines(path, 80)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, "log "+name+": "+err.Error())
			continue
		}
		for i := range lines {
			lines[i] = redactSupportText(lines[i])
		}
		out[name] = lines
	}
	return out, errs
}

func tailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		n = 80
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}

var (
	telegramTokenRe    = regexp.MustCompile(`\b\d{5,}:[A-Za-z0-9_-]{20,}\b`)
	promptAssignmentRe = regexp.MustCompile(`(?i)\b(prompt|input_json|input|payload|text)=("[^"]*"|[^ \t\n]+)`)
)

func redactSupportBundle(body []byte, home string) []byte {
	s := string(body)
	s = telegramTokenRe.ReplaceAllString(s, "[REDACTED]")
	s = approval.Scrub(s)
	if home != "" {
		s = strings.ReplaceAll(s, filepath.Clean(home), "~")
	}
	return []byte(s)
}

func redactSupportText(s string) string {
	s = telegramTokenRe.ReplaceAllString(s, "[REDACTED]")
	s = promptAssignmentRe.ReplaceAllString(s, `${1}="[REDACTED]"`)
	return approval.Scrub(s)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
