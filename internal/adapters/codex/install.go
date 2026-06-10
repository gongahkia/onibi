package codex

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

const Agent = "codex"

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
	removeManaged(cfg)
	cfg[common.VersionField] = common.IntegrationVersion
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
			info.Message = "not installed"
			return info
		}
		info.Message = err.Error()
		return info
	}
	info.Installed = len(body) > 2
	if !info.Installed {
		info.Message = "not installed"
		return info
	}
	version := InstalledVersion(path)
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != "" && version != common.IntegrationVersion
	if err := common.VerifyRecorded(ctx, db, Agent, path, body); err != nil {
		info.Message = err.Error()
	} else {
		info.Message = "Codex hooks installed"
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
		return errors.New("onibi-managed Codex hook is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func hook(notifyBin string, e eventSpec) map[string]any {
	return map[string]any{
		common.GuardField:   true,
		common.VersionField: common.IntegrationVersion,
		"type":              "command",
		"command":           common.Command(notifyBin, Agent, Agent, e.typ, e.wait, e.response),
		"timeout":           e.timeout,
		"statusMessage":     "Waiting for Onibi",
	}
}

func ManagedBody(path string) ([]byte, error) {
	cfg, err := common.ReadJSON(path, map[string]any{"hooks": map[string]any{}})
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
