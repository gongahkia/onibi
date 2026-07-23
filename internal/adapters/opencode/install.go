package opencode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gongahkia/onibi/internal/adapters/catalog"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/store"
)

const (
	Agent                  = "opencode"
	MinimumProviderVersion = "1.18.3"
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
	}, map[string]string{"PreToolUse": "*"}))
}

var pluginEvents = []string{"event", "tool.execute.before", "tool.execute.after", "session.idle"}

func PluginPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ONIBI_OPENCODE_PLUGIN")); v != "" {
		return filepath.Abs(v)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ONIBI_OPENCODE_SCOPE")), "project") {
		return filepath.Abs(filepath.Join(".opencode", "plugins", "onibi.js"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "plugins", "onibi.js"), nil
}

func DetectPresence() bool {
	if path, err := PluginPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".config", "opencode") || common.HomeExists(".opencode")
}

func Install(ctx context.Context, db *store.DB, notifyBin string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}
	path, err := PluginPath()
	if err != nil {
		return err
	}
	body := []byte(pluginSource(notifyBin))
	if current, err := os.ReadFile(path); err == nil && string(current) == string(body) {
		if err := common.VerifyRecorded(ctx, db, Agent, path, current); err == nil {
			return nil
		}
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	if err := common.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return common.Record(ctx, db, Agent, path, body)
}

func Uninstall(ctx context.Context, db *store.DB) error {
	path, err := PluginPath()
	if err != nil {
		return err
	}
	if body, err := os.ReadFile(path); err == nil {
		if !strings.Contains(string(body), `const ONIBI_AGENT = "opencode";`) || common.VerifyRecorded(ctx, db, Agent, path, body) != nil {
			if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return common.DeleteRecord(ctx, db, Agent, path)
}

func Status(ctx context.Context, db *store.DB) common.Info {
	path, err := PluginPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, MinimumProviderVersion: MinimumProviderVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, MinimumProviderVersion: MinimumProviderVersion, InstallPath: path}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			common.MarkNotInstalled(&info)
			return info
		}
		info.Message = err.Error()
		return info
	}
	src := string(body)
	version := installedVersion(src)
	info.Installed = strings.Contains(src, "ONIBI_AGENT = \"opencode\"")
	if !info.Installed {
		if strings.Contains(src, "onibi-notify") {
			info.Message = "unmanaged onibi-like hook; run onibi agent install --agent opencode to adopt"
			info.Next = "onibi agent install --agent opencode"
		} else {
			common.MarkNotInstalled(&info)
		}
		return info
	}
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "OpenCode plugin installed", "onibi agent install --agent opencode")
	return info
}

func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := PluginPath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), "ONIBI_AGENT = \"opencode\"") {
		return errors.New("onibi-managed OpenCode plugin is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func Adopt(ctx context.Context, db *store.DB) error {
	path, err := PluginPath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), "ONIBI_AGENT = \"opencode\"") {
		return errors.New("onibi-managed OpenCode plugin is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func ExpectedHooks(notifyBin string) ([]common.ExpectedHook, error) {
	out := make([]common.ExpectedHook, 0, len(pluginEvents))
	for _, event := range pluginEvents {
		out = append(out, common.ExpectedHook{Event: event, Type: "plugin", Command: notifyBin})
	}
	return out, nil
}

func ObservedHooks() ([]common.ObservedHook, error) {
	path, err := PluginPath()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(body)
	managed := strings.Contains(src, `const ONIBI_AGENT = "opencode";`)
	notify, notifyOK := pluginNotify(src)
	if !strings.Contains(src, "export const Onibi = async () => ({") {
		return []common.ObservedHook{{Event: "*", Managed: managed, Problems: []string{"schema-invalid: Onibi plugin export missing"}}}, nil
	}
	var out []common.ObservedHook
	if !notifyOK {
		out = append(out, common.ObservedHook{Event: "*", Managed: managed, Problems: []string{"schema-invalid: ONIBI_NOTIFY string constant missing"}})
	}
	for _, event := range pluginEvents {
		if strings.Contains(src, pluginHookSignature(event)) {
			out = append(out, common.ObservedHook{Event: event, Type: "plugin", Command: notify, Managed: managed})
		}
	}
	return out, nil
}

func BackupPath(ctx context.Context, db *store.DB) string {
	path, err := PluginPath()
	if err != nil {
		return ""
	}
	backup, ok, err := common.LatestBackup(ctx, db, Agent, path)
	if err != nil || !ok {
		return ""
	}
	return backup.BackupPath
}

func TrustInstructions() []string {
	return []string{
		"OpenCode next step: restart OpenCode or start a new session so local plugins are loaded from the plugin directory.",
		"Use ONIBI_OPENCODE_SCOPE=project for .opencode/plugins/onibi.js, or ONIBI_OPENCODE_PLUGIN for an explicit plugin path.",
	}
}

func installedVersion(src string) string {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "const ONIBI_INTEGRATION_VERSION = ") {
			return strings.Trim(strings.TrimSuffix(strings.TrimPrefix(line, "const ONIBI_INTEGRATION_VERSION = "), ";"), `"`)
		}
	}
	return ""
}

func pluginNotify(src string) (string, bool) {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "const ONIBI_NOTIFY = ") {
			continue
		}
		value := strings.TrimSuffix(strings.TrimPrefix(line, "const ONIBI_NOTIFY = "), ";")
		path, err := strconv.Unquote(value)
		return path, err == nil && path != ""
	}
	return "", false
}

func pluginHookSignature(event string) string {
	switch event {
	case "event":
		return "event: async (input)"
	case "tool.execute.before":
		return `"tool.execute.before": async (input, output)`
	case "tool.execute.after":
		return `"tool.execute.after": async (input, output)`
	case "session.idle":
		return `"session.idle": async (input)`
	default:
		return ""
	}
}

func pluginSource(notifyBin string) string {
	return fmt.Sprintf(`const ONIBI_INTEGRATION_VERSION = "%s";
const ONIBI_AGENT = "opencode";
const ONIBI_NOTIFY = %q;

async function runOnibi(args, payload) {
  const env = globalThis.process?.env ?? {};
  if (!env.ONIBI_SESSION_ID) return { code: 0, stdout: "", stderr: "" };
  const body = JSON.stringify(payload ?? {});
  if (globalThis.Bun?.spawn) {
    const p = Bun.spawn([ONIBI_NOTIFY, ...args], { stdin: "pipe", stdout: "pipe", stderr: "pipe" });
    p.stdin.write(body);
    p.stdin.end();
    const stdout = await new Response(p.stdout).text();
    const stderr = await new Response(p.stderr).text();
    const code = await p.exited;
    return { code, stdout, stderr };
  }
  const { spawnSync } = await import("node:child_process");
  const r = spawnSync(ONIBI_NOTIFY, args, { input: body, encoding: "utf8" });
  return { code: r.status ?? 0, stdout: r.stdout ?? "", stderr: r.stderr ?? "" };
}

async function emit(type, event, extra = {}) {
  await runOnibi(["--agent", ONIBI_AGENT, "--format", "opencode", "--type", type], { event, ...extra }).catch(() => {});
}

async function emitEvent(input) {
  const event = input?.event ?? input;
  const name = event?.type ?? event?.event ?? "";
  if (name === "session.deleted") return emit("session_exited", event);
  if (name === "session.idle") return emit("agent_done", event);
  return emit("agent_message", event);
}

async function approval(input, output) {
  const payload = {
    hook_event_name: "tool.execute.before",
    session_id: input?.sessionID ?? input?.sessionId ?? input?.session?.id,
    cwd: input?.cwd ?? input?.directory ?? input?.project?.root,
    tool_name: input?.tool ?? input?.toolName ?? output?.tool ?? "unknown",
    tool_input: output?.args ?? input?.args ?? input?.toolArgs ?? {},
    raw: { input, output }
  };
  const r = await runOnibi(["--agent", ONIBI_AGENT, "--format", "opencode", "--type", "approval_request", "--wait", "--response", "onibi-json"], payload);
  if (!r.stdout.trim()) return;
  const decision = JSON.parse(r.stdout);
  if (decision.decision === "deny" || decision.decision === "expired") throw new Error(decision.reason || "Denied by Onibi");
  if (decision.decision === "edited" && decision.updated_input) output.args = parseUpdatedInput(decision.updated_input);
}

function parseUpdatedInput(value) {
  const parsed = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Onibi edited input must be a JSON object");
  return parsed;
}

export const Onibi = async () => ({
  event: async (input) => emitEvent(input),
  "tool.execute.before": async (input, output) => {
    await emit("agent_message", { input, output });
    await approval(input, output);
  },
  "tool.execute.after": async (input, output) => emit("agent_message", { input, output }),
  "session.idle": async (input) => emit("agent_done", input)
});

export default Onibi;
`, common.IntegrationVersion, notifyBin)
}
