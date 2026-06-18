package common

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

const (
	IntegrationVersion = "2.0.0"
	VersionField       = "onibiIntegrationVersion"
	GuardField         = "onibiManaged"
)

type Info struct {
	Name             string
	Support          string
	Installed        bool
	Managed          bool
	HashRecorded     bool
	Tampered         bool
	Adoptable        bool
	InstalledVersion *string
	BundledVersion   string
	Outdated         bool
	InstallPath      string
	Message          string
	Next             string
}

type HookRecord struct {
	Agent       string
	Path        string
	SHA256      string
	Version     string
	InstalledAt int64
}

type HookBackup struct {
	Agent        string `json:"agent"`
	Path         string `json:"path"`
	SourceSHA256 string `json:"source_sha256"`
	BackupPath   string `json:"backup_path"`
	CreatedAt    int64  `json:"created_at"`
}

type ExpectedHook struct {
	Event         string `json:"event"`
	Matcher       string `json:"matcher,omitempty"`
	Type          string `json:"type"`
	Command       string `json:"command"`
	Timeout       int    `json:"timeout,omitempty"`
	StatusMessage string `json:"status_message,omitempty"`
}

type ObservedHook struct {
	Event         string   `json:"event"`
	Matcher       string   `json:"matcher,omitempty"`
	Type          string   `json:"type,omitempty"`
	Command       string   `json:"command,omitempty"`
	Timeout       int      `json:"timeout,omitempty"`
	StatusMessage string   `json:"status_message,omitempty"`
	Managed       bool     `json:"managed"`
	Problems      []string `json:"problems,omitempty"`
}

func VersionPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func HomePath(env string, parts ...string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return filepath.Abs(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, parts...)...), nil
}

func Command(notifyBin, agent, format, typ string, wait bool, response string) string {
	return guardedCommand(notifyBin, agent, format, typ, wait, response, true)
}

func UnguardedCommand(notifyBin, agent, format, typ string, wait bool, response string) string {
	return guardedCommand(notifyBin, agent, format, typ, wait, response, false)
}

func guardedCommand(notifyBin, agent, format, typ string, wait bool, response string, requireSession bool) string {
	args := []string{strconv.Quote(notifyBin), "--agent", agent, "--format", format}
	if typ != "" {
		args = append(args, "--type", typ)
	}
	if wait {
		args = append(args, "--wait")
	}
	if response != "" {
		args = append(args, "--response", response)
	}
	cmd := strings.Join(args, " ")
	if requireSession {
		return `if [ -z "${ONIBI_SESSION_ID:-}" ]; then exit 0; fi; exec ` + cmd
	}
	return cmd
}

func ReadJSON(path string, fallback map[string]any) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cloneMap(fallback), nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return cloneMap(fallback), nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		return map[string]any{}, nil
	}
	return m, nil
}

func WriteJSON(path string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(b, '\n'), 0o600)
}

func WriteFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func Record(ctx context.Context, db *store.DB, agent, path string, body []byte) error {
	sum := sha256.Sum256(body)
	_, err := db.SQL().ExecContext(ctx,
		`INSERT INTO hooks(agent, path, sha256, version, installed_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(agent, path) DO UPDATE SET sha256=excluded.sha256, version=excluded.version, installed_at=excluded.installed_at`,
		agent, path, hex.EncodeToString(sum[:]), IntegrationVersion, time.Now().Unix())
	return err
}

func RecordFor(ctx context.Context, db *store.DB, agent, path string) (HookRecord, bool, error) {
	var rec HookRecord
	err := db.SQL().QueryRowContext(ctx,
		`SELECT agent, path, sha256, COALESCE(version, ''), installed_at FROM hooks WHERE agent = ? AND path = ?`,
		agent, path).Scan(&rec.Agent, &rec.Path, &rec.SHA256, &rec.Version, &rec.InstalledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return HookRecord{}, false, nil
	}
	if err != nil {
		return HookRecord{}, false, err
	}
	return rec, true, nil
}

func DeleteRecord(ctx context.Context, db *store.DB, agent, path string) error {
	_, err := db.SQL().ExecContext(ctx, `DELETE FROM hooks WHERE agent = ? AND path = ?`, agent, path)
	return err
}

func BackupOriginal(ctx context.Context, db *store.DB, agent, path string) (string, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	sourceSHA := hex.EncodeToString(sum[:])
	var existing string
	err = db.SQL().QueryRowContext(ctx,
		`SELECT backup_path FROM hook_backups WHERE agent = ? AND path = ? AND source_sha256 = ?`,
		agent, path, sourceSHA).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	prefix := sourceSHA[:12]
	backupPath := filepath.Join(filepath.Dir(db.Path()), "hook-backups", agent, prefix, filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err == nil {
		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
	}
	_, err = db.SQL().ExecContext(ctx,
		`INSERT OR IGNORE INTO hook_backups(agent, path, source_sha256, backup_path, created_at) VALUES (?, ?, ?, ?, ?)`,
		agent, path, sourceSHA, backupPath, time.Now().Unix())
	return backupPath, err
}

func LatestBackup(ctx context.Context, db *store.DB, agent, path string) (HookBackup, bool, error) {
	var b HookBackup
	err := db.SQL().QueryRowContext(ctx,
		`SELECT agent, path, source_sha256, backup_path, created_at FROM hook_backups WHERE agent = ? AND path = ? ORDER BY created_at DESC LIMIT 1`,
		agent, path).Scan(&b.Agent, &b.Path, &b.SourceSHA256, &b.BackupPath, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return HookBackup{}, false, nil
	}
	if err != nil {
		return HookBackup{}, false, err
	}
	return b, true, nil
}

func VerifyRecorded(ctx context.Context, db *store.DB, agent, path string, body []byte) error {
	recorded, tampered, got, want, err := RecordState(ctx, db, agent, path, body)
	if err != nil {
		return err
	}
	if !recorded {
		return fmt.Errorf("managed hook hash missing")
	}
	if tampered {
		return fmt.Errorf("managed hook tampered: have %s want %s", got, want)
	}
	return nil
}

func RecordState(ctx context.Context, db *store.DB, agent, path string, body []byte) (recorded, tampered bool, got, want string, err error) {
	err = db.SQL().QueryRowContext(ctx,
		`SELECT sha256 FROM hooks WHERE agent = ? AND path = ?`, agent, path).Scan(&want)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, "", "", nil
	}
	if err != nil {
		return false, false, "", "", err
	}
	sum := sha256.Sum256(body)
	got = hex.EncodeToString(sum[:])
	return true, got != want, got, want, nil
}

func ApplyManagedStatus(ctx context.Context, db *store.DB, info *Info, agent, path string, body []byte, okMessage, next string) {
	info.Installed = true
	info.Managed = true
	info.Next = ""
	recorded, tampered, _, _, err := RecordState(ctx, db, agent, path, body)
	if err != nil {
		info.Message = err.Error()
		return
	}
	info.HashRecorded = recorded
	info.Tampered = tampered
	if !recorded {
		info.Adoptable = true
		info.Message = "managed, hash missing; run " + next + " to adopt"
		info.Next = next
		return
	}
	if tampered {
		info.Message = "managed, tampered; review " + path + " then run " + next
		info.Next = next
		return
	}
	if info.Outdated {
		info.Message = "managed, outdated; run " + next
		info.Next = next
		return
	}
	info.Message = okMessage
}

func MarkNotInstalled(info *Info) {
	info.Installed = false
	info.Managed = false
	info.HashRecorded = false
	info.Tampered = false
	info.Adoptable = false
	info.Message = "not installed"
	info.Next = ""
}

func StableJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func SortStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
