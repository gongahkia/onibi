package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

// SettingsPath returns the canonical Claude Code settings.json path:
// $CLAUDE_CONFIG_DIR/settings.json or ~/.claude/settings.json.
func SettingsPath() (string, error) {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// guardKey is the top-level key we add to the settings hooks block so we
// can find and remove our entries idempotently.
const guardKey = "onibi-managed"

// Install writes the Onibi Stop and PreToolUse hooks into Claude's
// settings.json. Idempotent — previous Onibi-managed entries are replaced
// in place. Other (user-managed) hooks are preserved verbatim.
//
// notifyBin must be an absolute path to onibi-notify.
func Install(ctx context.Context, db *store.DB, notifyBin string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}

	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	existing, err := readJSON(path)
	if err != nil {
		return err
	}

	stop := buildStopHook(notifyBin)
	pre := buildPreToolUseHook(notifyBin)

	merged := mergeEventHook(existing, "Stop", stop)
	merged = mergeEventHook(merged, "PreToolUse", pre)
	if err := writeJSON(path, merged); err != nil {
		return err
	}

	// record hash for tamper detection (TODO §7.3, T9). The hash covers
	// both managed entries concatenated so any tampering of either is
	// detected.
	combined := struct {
		Stop, PreToolUse map[string]any
	}{stop, pre}
	body, _ := json.Marshal(combined)
	sum := sha256.Sum256(body)
	_, err = db.SQL().ExecContext(ctx,
		`INSERT INTO hooks(agent, path, sha256, installed_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(agent, path) DO UPDATE SET sha256=excluded.sha256, installed_at=excluded.installed_at`,
		"claude", path, hex.EncodeToString(sum[:]), time.Now().Unix())
	return err
}

// Uninstall removes all Onibi-managed hooks from Claude's settings.json.
func Uninstall(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	existing, err := readJSON(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	cleaned := removeEventHook(existing, "Stop")
	cleaned = removeEventHook(cleaned, "PreToolUse")
	if err := writeJSON(path, cleaned); err != nil {
		return err
	}
	_, err = db.SQL().ExecContext(ctx, `DELETE FROM hooks WHERE agent = ? AND path = ?`, "claude", path)
	return err
}

// VerifyHash returns nil iff the currently installed hook block (Stop +
// PreToolUse) matches what was recorded at install time.
func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	row := db.SQL().QueryRowContext(ctx,
		`SELECT sha256 FROM hooks WHERE agent = ? AND path = ?`, "claude", path)
	var want string
	if err := row.Scan(&want); err != nil {
		return fmt.Errorf("no installed hash on record: %w", err)
	}
	existing, err := readJSON(path)
	if err != nil {
		return err
	}
	stop := extractEventHook(existing, "Stop")
	pre := extractEventHook(existing, "PreToolUse")
	if stop == nil || pre == nil {
		return errors.New("onibi-managed Stop or PreToolUse hook is missing")
	}
	combined := struct {
		Stop, PreToolUse map[string]any
	}{stop, pre}
	body, _ := json.Marshal(combined)
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("hook tampered: have %s want %s", got, want)
	}
	return nil
}

// ----------------------------------------------------------------------------
// JSON I/O + hook merge logic
// ----------------------------------------------------------------------------

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSON(path string, m map[string]any) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// buildStopHook returns the JSON shape Claude Code expects for one
// HookMatcher in the Stop event. Empty matcher = match all, one command.
// Tagged with guardKey so we can locate the entry on re-install or remove.
func buildStopHook(notifyBin string) map[string]any {
	return map[string]any{
		guardKey:  true,
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": notifyBin + " --type agent_done",
				"timeout": 5,
			},
		},
	}
}

// buildPreToolUseHook installs an empty-matcher hook that intercepts every
// tool call. The hook runs in --wait mode so Claude blocks until the
// daemon returns a decision (approve/deny/edited).
//
// timeout intentionally longer than the approval TTL — Claude waits up to
// `timeout` seconds for the hook to exit. If it expires before we respond,
// Claude treats the hook as failing-closed (denies). Approval TTL is 5min
// so 360 (6min) gives slack for round-trip.
func buildPreToolUseHook(notifyBin string) map[string]any {
	return map[string]any{
		guardKey:  true,
		"matcher": "",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": notifyBin + " --type approval_request --wait",
				"timeout": 360,
			},
		},
	}
}

// mergeEventHook adds our entry to settings.hooks.<eventName>, replacing
// any prior Onibi-managed entry and leaving user-managed entries untouched.
func mergeEventHook(settings map[string]any, eventName string, ours map[string]any) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	existing, _ := hooks[eventName].([]any)
	kept := existing[:0]
	for _, e := range existing {
		m, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		if m[guardKey] == true {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, ours)
	hooks[eventName] = kept
	settings["hooks"] = hooks
	return settings
}

func removeEventHook(settings map[string]any, eventName string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return settings
	}
	existing, _ := hooks[eventName].([]any)
	kept := existing[:0]
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok && m[guardKey] == true {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(hooks, eventName)
	} else {
		hooks[eventName] = kept
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	return settings
}

func extractEventHook(settings map[string]any, eventName string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	existing, _ := hooks[eventName].([]any)
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok && m[guardKey] == true {
			return m
		}
	}
	return nil
}
