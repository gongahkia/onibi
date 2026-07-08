package copilot

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

const Agent = "copilot"

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
	typ      string
	wait     bool
	response string
	timeout  int
}

var events = []eventSpec{
	{event: "sessionStart", typ: "agent_message", timeout: 30},
	{event: "userPromptSubmitted", typ: "agent_message", timeout: 30},
	{event: "preToolUse", typ: "approval_request", wait: true, response: "provider", timeout: 360},
	{event: "postToolUse", typ: "agent_message", timeout: 30},
	{event: "postToolUseFailure", typ: "agent_message", timeout: 30},
	{event: "notification", typ: "agent_message", timeout: 30},
	{event: "agentStop", typ: "agent_done", timeout: 30},
	{event: "sessionEnd", typ: "session_exited", timeout: 30},
	{event: "errorOccurred", typ: "agent_message", timeout: 30},
}

func HooksPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ONIBI_COPILOT_HOOK")); v != "" {
		return filepath.Abs(v)
	}
	if h := strings.TrimSpace(os.Getenv("COPILOT_HOME")); h != "" {
		return filepath.Abs(filepath.Join(h, "hooks", "onibi.json"))
	}
	return common.HomePath("ONIBI_COPILOT_HOOK", ".copilot", "hooks", "onibi.json")
}

func DetectPresence() bool {
	if path, err := HooksPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".copilot")
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
		"version": 1,
		"hooks":   map[string]any{},
	}
	hooks := cfg["hooks"].(map[string]any)
	for _, e := range events {
		hooks[e.event] = []any{hook(notifyBin, e)}
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
	version := installedVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Copilot CLI hooks installed", "onibi install-hooks --agent copilot")
	if disabled, _ := disableAllHooks(path); disabled {
		info.Disabled = true
		info.Message = "Copilot disableAllHooks=true; installed hooks are skipped"
		info.Next = "set disableAllHooks=false in " + path
	}
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
		return errors.New("onibi-managed Copilot hook is missing")
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
		return errors.New("onibi-managed Copilot hook is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(events))
	for _, e := range events {
		h := hook(notifyBin, e)
		cmd, _ := h["bash"].(string)
		out = append(out, common.ExpectedHook{
			Event:   e.event,
			Type:    "command",
			Command: cmd,
			Timeout: e.timeout,
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
		switch k {
		case "version", "disableAllHooks", "hooks":
		default:
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
		for _, h := range asSlice(hooks[event]) {
			hm, ok := h.(map[string]any)
			if !ok {
				out = append(out, common.ObservedHook{Event: event, Problems: []string{"schema-invalid: hook is not an object"}})
				continue
			}
			typ, _ := hm["type"].(string)
			matcher, _ := hm["matcher"].(string)
			out = append(out, common.ObservedHook{
				Event:    event,
				Matcher:  matcher,
				Type:     typ,
				Command:  commandValue(hm),
				Timeout:  intValue(firstValue(hm, "timeoutSec", "timeout")),
				Managed:  isManaged(hm),
				Problems: hookSchemaProblems(hm),
			})
		}
	}
	return out, nil
}

func TrustInstructions() []string {
	return []string{
		"Copilot next step: restart Copilot CLI so hook configurations are loaded.",
		"Copilot loads user hooks from ~/.copilot/hooks/*.json, or $COPILOT_HOME/hooks/*.json when COPILOT_HOME is set.",
		"If disableAllHooks is true, Copilot skips hooks in that file.",
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
		"type":       "command",
		"bash":       common.VersionedCommand(notifyBin, Agent, Agent, e.typ, e.wait, e.response),
		"timeoutSec": e.timeout,
	}
}

func ManagedBody(path string) ([]byte, error) {
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return nil, err
	}
	managed := []any{}
	if hooks, ok := cfg["hooks"].(map[string]any); ok {
		for _, event := range common.SortStrings(keys(hooks)) {
			for _, h := range asSlice(hooks[event]) {
				if isManaged(h) {
					managed = append(managed, map[string]any{"event": event, "hook": h})
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
		for _, entries := range hooks {
			for _, h := range asSlice(entries) {
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
	return ""
}

func disableAllHooks(path string) (bool, error) {
	cfg, err := common.ReadJSON(path, map[string]any{})
	if err != nil {
		return false, err
	}
	v, _ := cfg["disableAllHooks"].(bool)
	return v, nil
}

func isManaged(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if m[common.GuardField] == true {
		return true
	}
	cmd, _ := m["bash"].(string)
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent copilot")
}

func commandValue(m map[string]any) string {
	cmd, _ := m["bash"].(string)
	if cmd != "" {
		return cmd
	}
	cmd, _ = m["command"].(string)
	return cmd
}

func hookSchemaProblems(m map[string]any) []string {
	var problems []string
	for k := range m {
		switch k {
		case "type", "bash", "powershell", "command", "cwd", "env", "timeout", "timeoutSec", "matcher":
		default:
			problems = append(problems, "schema-invalid: unknown hook field "+k)
		}
	}
	if typ, _ := m["type"].(string); typ != "" && typ != "command" {
		problems = append(problems, "schema-invalid: unsupported hook type "+typ)
	}
	return problems
}

func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case jsonNumber:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}

type jsonNumber interface {
	Int64() (int64, error)
}

func firstValue(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
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
