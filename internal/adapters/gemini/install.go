package gemini

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

const (
	Agent                  = "gemini"
	MinimumProviderVersion = "0.43.0"
)

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
	}, map[string]string{"BeforeTool": "*"}))
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
	{event: "SessionStart", typ: "agent_message", timeout: 30000},
	{event: "BeforeAgent", typ: "agent_message", timeout: 30000},
	{event: "BeforeTool", matcher: "*", typ: "approval_request", wait: true, response: "provider", timeout: 360000},
	{event: "AfterTool", typ: "agent_message", timeout: 30000},
	{event: "Notification", typ: "agent_message", timeout: 30000},
	{event: "AfterAgent", typ: "agent_done", timeout: 30000},
	{event: "SessionEnd", typ: "session_exited", timeout: 30000},
}

func SettingsPath() (string, error) {
	return common.HomePath("ONIBI_GEMINI_SETTINGS", ".gemini", "settings.json")
}

func DetectPresence() bool {
	if path, err := SettingsPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".gemini")
}

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
	if err := VerifyHash(ctx, db); err == nil && installedVersion(path) == common.IntegrationVersion {
		return nil
	}
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
	if err != nil {
		return err
	}
	if err := VerifyHash(ctx, db); err != nil {
		if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
			return err
		}
	}
	removeManaged(cfg)
	delete(cfg, common.VersionField)
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		cfg["hooks"] = hooks
	}
	for _, e := range events {
		group := map[string]any{"hooks": []any{hook(notifyBin, e)}}
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
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return common.DeleteRecord(ctx, db, Agent, path)
	} else if err != nil {
		return err
	}
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
	if err != nil {
		return err
	}
	if err := VerifyHash(ctx, db); err != nil {
		if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
			return err
		}
	}
	removeManaged(cfg)
	delete(cfg, common.VersionField)
	if err := common.WriteJSON(path, cfg); err != nil {
		return err
	}
	return common.DeleteRecord(ctx, db, Agent, path)
}

func Status(ctx context.Context, db *store.DB) common.Info {
	path, err := SettingsPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, MinimumProviderVersion: MinimumProviderVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, MinimumProviderVersion: MinimumProviderVersion, InstallPath: path}
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
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Gemini hooks installed", "onibi agent install --agent gemini")
	if geminiHooksDisabled(path) {
		info.Disabled = true
		info.Message = "Gemini hooks disabled by hooksConfig.enabled=false"
		info.Next = "enable hooksConfig.enabled in " + path
	}
	return info
}

func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	if len(body) <= 2 {
		return errors.New("onibi-managed Gemini hook is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func Adopt(ctx context.Context, db *store.DB) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	body, err := ManagedBody(path)
	if err != nil {
		return err
	}
	if len(body) <= 2 {
		return errors.New("onibi-managed Gemini hook is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(events))
	for _, e := range events {
		h := hook(notifyBin, e)
		cmd, _ := h["command"].(string)
		out = append(out, common.ExpectedHook{Event: e.event, Matcher: e.matcher, Type: "command", Command: cmd, Timeout: e.timeout})
	}
	return out, nil
}

func ObservedHooks() ([]common.ObservedHook, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return nil, err
	}
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		if _, exists := cfg["hooks"]; exists {
			return []common.ObservedHook{{Event: "*", Problems: []string{"schema-invalid: hooks is not an object"}}}, nil
		}
		return nil, nil
	}
	var out []common.ObservedHook
	for _, event := range common.SortStrings(keys(hooks)) {
		for _, group := range asSlice(hooks[event]) {
			gm, ok := group.(map[string]any)
			if !ok {
				out = append(out, common.ObservedHook{Event: event, Problems: []string{"schema-invalid: matcher group is not an object"}})
				continue
			}
			matcher, _ := gm["matcher"].(string)
			problems := groupSchemaProblems(gm)
			entries, ok := gm["hooks"].([]any)
			if !ok {
				out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(problems, "schema-invalid: hooks is not an array")})
				continue
			}
			for _, entry := range entries {
				hook, ok := entry.(map[string]any)
				if !ok {
					out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Problems: append(problems, "schema-invalid: hook is not an object")})
					continue
				}
				typ, _ := hook["type"].(string)
				cmd, _ := hook["command"].(string)
				out = append(out, common.ObservedHook{Event: event, Matcher: matcher, Type: typ, Command: cmd, Timeout: intValue(hook["timeout"]), Managed: isManaged(hook), Problems: append(problems, hookSchemaProblems(hook)...)})
			}
		}
	}
	return out, nil
}

func TrustInstructions() []string {
	return []string{
		"Gemini CLI next step: inspect the configured hooks and verify the Onibi commands before use.",
		"Use onibi agent inspect --agent gemini to compare expected commands, installed commands, backups, and drift.",
	}
}

func BackupPath(ctx context.Context, db *store.DB) string {
	path, err := SettingsPath()
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
		"command": common.VersionedCommand(notifyBin, Agent, Agent, e.typ, e.wait, e.response),
		"timeout": e.timeout,
	}
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
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent gemini")
}

func commandValue(m map[string]any) string {
	cmd, _ := m["command"].(string)
	return cmd
}

func geminiHooksDisabled(path string) bool {
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return false
	}
	hooksConfig, _ := cfg["hooksConfig"].(map[string]any)
	enabled, exists := hooksConfig["enabled"].(bool)
	return exists && !enabled
}

func groupSchemaProblems(group map[string]any) []string {
	var problems []string
	for key, value := range group {
		switch key {
		case "hooks":
		case "matcher":
			if _, ok := value.(string); !ok {
				problems = append(problems, "schema-invalid: matcher is not a string")
			}
		case "sequential":
			if _, ok := value.(bool); !ok {
				problems = append(problems, "schema-invalid: sequential is not a boolean")
			}
		default:
			problems = append(problems, "schema-invalid: unknown matcher field "+key)
		}
	}
	return problems
}

func hookSchemaProblems(hook map[string]any) []string {
	var problems []string
	for key, value := range hook {
		switch key {
		case "type", "command", "name", "description":
			if _, ok := value.(string); !ok {
				problems = append(problems, "schema-invalid: "+key+" is not a string")
			}
		case "timeout":
			if intValue(value) <= 0 {
				problems = append(problems, "schema-invalid: timeout is not a positive number")
			}
		default:
			problems = append(problems, "schema-invalid: unknown hook field "+key)
		}
	}
	return problems
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
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
