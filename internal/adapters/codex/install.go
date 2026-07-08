package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/catalog"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

const Agent = "codex"
const versionEnv = common.VersionEnv

func init() {
	catalog.MustRegister(catalog.BuiltinAgentManifest(Agent, catalog.Adapter{
		Name:              Agent,
		Install:           Install,
		Uninstall:         Uninstall,
		Status:            Status,
		Verify:            VerifyHash,
		Adopt:             Adopt,
		ExpectedHooks:     ExpectedHooks,
		ObservedHooks:     ObservedHooks,
		TrustInstructions: TrustInstructions,
		BackupPath:        BackupPath,
		DetectPresence:    DetectPresence,
	}, map[string]string{"PreToolUse": "*"}))
}

type eventSpec struct {
	event    string
	matcher  string
	typ      string
	wait     bool
	response string
	timeout  int
}

var events = []eventSpec{
	{event: "SessionStart", typ: "agent_message", timeout: 30},
	{event: "PreToolUse", typ: "approval_request", wait: true, response: "provider", timeout: 360},
	{event: "PostToolUse", typ: "agent_message", timeout: 30},
	{event: "Stop", typ: "agent_done", timeout: 30},
}

func HooksPath() (string, error) {
	return common.HomePath("ONIBI_CODEX_HOOKS", ".codex", "hooks.json")
}

func DetectPresence() bool {
	if path, err := HooksPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".config", "codex")
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
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
	if err != nil {
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	removeManaged(cfg)
	delete(cfg, common.VersionField)
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		cfg["hooks"] = hooks
	}
	for _, e := range events {
		group := map[string]any{
			"hooks": []any{hook(notifyBin, e)},
		}
		if e.matcher != "" {
			group["matcher"] = e.matcher
		}
		groups, _ := hooks[e.event].([]any)
		hooks[e.event] = append(groups, group)
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
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	removeManaged(cfg)
	delete(cfg, common.VersionField)
	if err := common.WriteJSON(path, cfg); err != nil {
		return err
	}
	return common.DeleteRecord(ctx, db, Agent, path)
}

func Status(ctx context.Context, db *store.DB) common.Info {
	path, err := HooksPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, InstallPath: path}
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
	version := InstalledVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Codex hooks installed", "onibi install-hooks --agent codex")
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
		return errors.New("onibi-managed Codex hook is missing")
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
		return errors.New("onibi-managed Codex hook is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(events))
	for _, e := range events {
		h := hook(notifyBin, e)
		cmd, _ := h["command"].(string)
		status, _ := h["statusMessage"].(string)
		out = append(out, common.ExpectedHook{
			Event:         e.event,
			Matcher:       e.matcher,
			Type:          "command",
			Command:       cmd,
			Timeout:       e.timeout,
			StatusMessage: status,
		})
	}
	return out, nil
}

func ObservedHooks() ([]common.ObservedHook, error) {
	path, err := HooksPath()
	if err != nil {
		return nil, err
	}
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
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
			for _, h := range asSlice(gm["hooks"]) {
				hm, ok := h.(map[string]any)
				if !ok {
					out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(groupProblems, "schema-invalid: hook is not an object")})
					continue
				}
				typ, _ := hm["type"].(string)
				cmd, _ := hm["command"].(string)
				status, _ := hm["statusMessage"].(string)
				out = append(out, common.ObservedHook{
					Event:         event,
					Matcher:       matcher,
					Type:          typ,
					Command:       cmd,
					Timeout:       intValue(hm["timeout"]),
					StatusMessage: status,
					Managed:       isManaged(hm),
					Problems:      append(groupProblems, hookSchemaProblems(hm)...),
				})
			}
		}
	}
	return out, nil
}

func TrustInstructions() []string {
	return []string{
		"Codex next step: run codex, choose Review hooks, inspect onibi-notify commands, then trust if they match.",
		"Do not choose Trust all unless you have inspected every command.",
		"Continue without trusting disables these Onibi hooks for that Codex run.",
	}
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
		"type":          "command",
		"command":       versionedCommand(notifyBin, e),
		"timeout":       e.timeout,
		"statusMessage": "Waiting for Onibi",
	}
}

func versionedCommand(notifyBin string, e eventSpec) string {
	return common.VersionedCommand(notifyBin, Agent, Agent, e.typ, e.wait, e.response)
}

func ManagedBody(path string) ([]byte, error) {
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
	if err != nil {
		return nil, err
	}
	managed := []any{}
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

func InstalledVersion(path string) string {
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
					if hm, ok := h.(map[string]any); ok && isManaged(h) {
						if s, _ := hm[common.VersionField].(string); s != "" {
							return s
						}
						if s := commandVersion(hm); s != "" {
							return s
						}
					}
				}
			}
		}
	}
	return ""
}

func commandVersion(h map[string]any) string {
	cmd, _ := h["command"].(string)
	return common.CommandVersion(cmd)
}

func removeManaged(cfg map[string]any) {
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for event, groups := range hooks {
		var keptGroups []any
		for _, group := range asSlice(groups) {
			gm, ok := group.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, group)
				continue
			}
			var keptHooks []any
			for _, h := range asSlice(gm["hooks"]) {
				if !isManaged(h) {
					keptHooks = append(keptHooks, h)
				}
			}
			if len(keptHooks) > 0 {
				gm["hooks"] = keptHooks
				keptGroups = append(keptGroups, gm)
			}
		}
		if len(keptGroups) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keptGroups
		}
	}
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
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent codex") ||
		strings.TrimSpace(cmd) == "onibi _hook codex"
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
		case "type", "command", "timeout", "statusMessage", "commandWindows", "command_windows", "async":
		default:
			problems = append(problems, "schema-invalid: unknown hook field "+k)
		}
	}
	return problems
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
