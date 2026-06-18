package goose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

const Agent = "goose"

type eventSpec struct {
	event string
	typ   string
}

var events = []eventSpec{
	{event: "SessionStart", typ: "agent_message"},
	{event: "SessionEnd", typ: "session_exited"},
	{event: "UserPromptSubmit", typ: "agent_message"},
	{event: "PreToolUse", typ: "agent_message"},
	{event: "PostToolUse", typ: "agent_message"},
	{event: "PostToolUseFailure", typ: "agent_message"},
	{event: "BeforeReadFile", typ: "agent_message"},
	{event: "AfterFileEdit", typ: "agent_message"},
	{event: "BeforeShellExecution", typ: "agent_message"},
	{event: "AfterShellExecution", typ: "agent_message"},
	{event: "Stop", typ: "agent_done"},
}

func HooksPath() (string, error) {
	return common.HomePath("ONIBI_GOOSE_HOOKS", ".agents", "plugins", "onibi", "hooks", "hooks.json")
}

func Install(ctx context.Context, db *store.DB, notifyBin string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}
	path, err := HooksPath()
	if err != nil {
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	cfg := map[string]any{
		"hooks": map[string]any{},
	}
	hooks := cfg["hooks"].(map[string]any)
	for _, e := range events {
		hooks[e.event] = []any{map[string]any{"hooks": []any{hook(notifyBin, e)}}}
	}
	if err := common.WriteJSON(path, cfg); err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	return common.Record(ctx, db, Agent, path, body)
}

func Uninstall(ctx context.Context, db *store.DB) error {
	path, err := HooksPath()
	if err != nil {
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return common.DeleteRecord(ctx, db, Agent, path)
}

func Status(ctx context.Context, db *store.DB) common.Info {
	path, err := HooksPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "event-bridge", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "event-bridge", BundledVersion: common.IntegrationVersion, InstallPath: path}
	body, err := ManagedBody(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			common.MarkNotInstalled(&info)
			return info
		}
		info.Message = err.Error()
		return info
	}
	info.Installed = len(body) > 2
	if !info.Installed {
		common.MarkNotInstalled(&info)
		return info
	}
	version := installedVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Goose lifecycle hooks installed", "onibi install-hooks --agent goose")
	return info
}

func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := HooksPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	if len(body) <= 2 {
		return errors.New("onibi-managed Goose hook is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func Adopt(ctx context.Context, db *store.DB) error {
	path, err := HooksPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	if len(body) <= 2 {
		return errors.New("onibi-managed Goose hook is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(events))
	for _, e := range events {
		h := hook(notifyBin, e)
		cmd, _ := h["command"].(string)
		out = append(out, common.ExpectedHook{
			Event:   e.event,
			Type:    "command",
			Command: cmd,
		})
	}
	return out, nil
}

func ObservedHooks() ([]common.ObservedHook, error) {
	path, err := HooksPath()
	if err != nil {
		return nil, err
	}
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return nil, err
	}
	var out []common.ObservedHook
	for k := range cfg {
		if k != "hooks" {
			out = append(out, common.ObservedHook{Event: "*", Problems: []string{"schema-invalid: unknown top-level field " + k}})
		}
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		if _, exists := cfg["hooks"]; exists {
			out = append(out, common.ObservedHook{Event: "*", Problems: []string{"schema-invalid: hooks is not an object"}})
		}
		return out, nil
	}
	for _, event := range common.SortStrings(keys(hooks)) {
		for _, group := range asSlice(hooks[event]) {
			gm, ok := group.(map[string]any)
			if !ok {
				out = append(out, common.ObservedHook{Event: event, Problems: []string{"schema-invalid: matcher group is not an object"}})
				continue
			}
			groupProblems := groupSchemaProblems(gm)
			matcher, _ := gm["matcher"].(string)
			inner := asSlice(gm["hooks"])
			if inner == nil {
				out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(groupProblems, "schema-invalid: hooks is not an array")})
				continue
			}
			for _, h := range inner {
				hm, ok := h.(map[string]any)
				if !ok {
					out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(groupProblems, "schema-invalid: hook is not an object")})
					continue
				}
				typ, _ := hm["type"].(string)
				cmd, _ := hm["command"].(string)
				out = append(out, common.ObservedHook{
					Event:    event,
					Matcher:  matcher,
					Type:     typ,
					Command:  cmd,
					Managed:  isManaged(hm),
					Problems: append(groupProblems, hookSchemaProblems(hm)...),
				})
			}
		}
	}
	return out, nil
}

func BackupPath(ctx context.Context, db *store.DB) string {
	path, err := HooksPath()
	if err != nil {
		return ""
	}
	backup, ok, err := common.LatestBackup(ctx, db, Agent, path)
	if err != nil || !ok {
		return ""
	}
	return backup.BackupPath
}

func hook(notifyBin string, e eventSpec) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": common.VersionedCommand(notifyBin, Agent, Agent, e.typ, false, ""),
	}
}

func ManagedBody(path string) ([]byte, error) {
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return nil, err
	}
	var managed []any
	if hooks, ok := cfg["hooks"].(map[string]any); ok {
		for _, event := range common.SortStrings(keys(hooks)) {
			for _, group := range asSlice(hooks[event]) {
				gm, _ := group.(map[string]any)
				for _, h := range asSlice(gm["hooks"]) {
					if isManaged(h) {
						managed = append(managed, map[string]any{"event": event, "hook": h})
					}
				}
			}
		}
	}
	return common.StableJSON(managed), nil
}

func installedVersion(path string) string {
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return ""
	}
	if s, _ := cfg[common.VersionField].(string); s != "" {
		return s
	}
	if hooks, ok := cfg["hooks"].(map[string]any); ok {
		for _, groups := range hooks {
			for _, group := range asSlice(groups) {
				gm, _ := group.(map[string]any)
				for _, h := range asSlice(gm["hooks"]) {
					hm, _ := h.(map[string]any)
					if isManaged(hm) {
						if s, _ := hm[common.VersionField].(string); s != "" {
							return s
						}
						if s := common.CommandVersion(commandValue(hm)); s != "" {
							return s
						}
					}
				}
			}
		}
	}
	return ""
}

func isManaged(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if m[common.GuardField] == true {
		return true
	}
	cmd, _ := m["command"].(string)
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent goose") ||
		strings.TrimSpace(cmd) == "onibi _hook goose"
}

func commandValue(m map[string]any) string {
	cmd, _ := m["command"].(string)
	return cmd
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func groupSchemaProblems(m map[string]any) []string {
	var problems []string
	for k := range m {
		switch k {
		case "matcher", "hooks":
		default:
			problems = append(problems, "schema-invalid: unknown matcher field "+k)
		}
	}
	return problems
}

func hookSchemaProblems(m map[string]any) []string {
	var problems []string
	for k := range m {
		switch k {
		case "type", "command", "prompt":
		default:
			problems = append(problems, "schema-invalid: unknown hook field "+k)
		}
	}
	return problems
}
