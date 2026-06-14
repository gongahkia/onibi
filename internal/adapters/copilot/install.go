package copilot

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

const Agent = "copilot"

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
	cfg := map[string]any{
		"version":           1,
		common.VersionField: common.IntegrationVersion,
		"hooks":             map[string]any{},
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

func hook(notifyBin string, e eventSpec) map[string]any {
	return map[string]any{
		common.GuardField:   true,
		common.VersionField: common.IntegrationVersion,
		"type":              "command",
		"bash":              common.Command(notifyBin, Agent, Agent, e.typ, e.wait, e.response),
		"timeoutSec":        e.timeout,
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
	cmd, _ := m["bash"].(string)
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent copilot")
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
