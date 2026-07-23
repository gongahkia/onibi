package amp

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

const Agent = "amp"

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

var pluginEvents = []string{"session.start", "agent.start", "tool.call", "tool.result", "agent.end"}

func PluginPath() (string, error) {
	return common.HomePath("ONIBI_AMP_PLUGIN", ".config", "amp", "plugins", "onibi.ts")
}

func DetectPresence() bool {
	if path, err := PluginPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".config", "amp") || common.HomeExists(".amp")
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
		if !strings.Contains(string(body), `const ONIBI_AGENT = "amp";`) || common.VerifyRecorded(ctx, db, Agent, path, body) != nil {
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
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	info := common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, InstallPath: path}
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
	info.Installed = strings.Contains(src, `ONIBI_AGENT = "amp"`)
	if !info.Installed {
		if strings.Contains(src, "onibi-notify") {
			info.Message = "unmanaged onibi-like hook; run onibi agent install --agent amp to adopt"
			info.Next = "onibi agent install --agent amp"
		} else {
			common.MarkNotInstalled(&info)
		}
		return info
	}
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Amp plugin installed", "onibi agent install --agent amp")
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
	if !strings.Contains(string(body), `ONIBI_AGENT = "amp"`) {
		return errors.New("onibi-managed Amp plugin is missing")
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
	if !strings.Contains(string(body), `ONIBI_AGENT = "amp"`) {
		return errors.New("onibi-managed Amp plugin is missing")
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
	managed := strings.Contains(src, `const ONIBI_AGENT = "amp";`)
	notify, notifyOK := pluginNotify(src)
	if !strings.Contains(src, "export default function (amp: PluginAPI)") {
		return []common.ObservedHook{{Event: "*", Managed: managed, Problems: []string{"schema-invalid: Amp plugin export missing"}}}, nil
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
		"Amp next step: run plugins: reload from the command palette, then plugins: list to confirm Onibi is loaded.",
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
	case "session.start", "agent.start", "tool.result", "agent.end":
		return `amp.on("` + event + `", async (event: any) => emit(`
	case "tool.call":
		return `amp.on("tool.call", async (event: any) => {`
	default:
		return ""
	}
}

func pluginSource(notifyBin string) string {
	return fmt.Sprintf(`import type { PluginAPI } from "@ampcode/plugin";

const ONIBI_INTEGRATION_VERSION = "%s";
const ONIBI_AGENT = "amp";
const ONIBI_NOTIFY = %q;

async function runOnibi(args: string[], payload: any) {
  const env = (globalThis as any).process?.env ?? {};
  if (!env.ONIBI_SESSION_ID) return { code: 0, stdout: "" };
  const body = JSON.stringify(payload ?? {});
  const bun = (globalThis as any).Bun;
  if (bun?.spawn) {
    const p = bun.spawn([ONIBI_NOTIFY, ...args], { stdin: "pipe", stdout: "pipe", stderr: "pipe" });
    p.stdin.write(body);
    p.stdin.end();
    const stdout = await new Response(p.stdout).text();
    const code = await p.exited;
    return { code, stdout };
  }
  const { spawnSync } = await import("node:child_process");
  const r = spawnSync(ONIBI_NOTIFY, args, { input: body, encoding: "utf8" });
  return { code: r.status ?? 0, stdout: r.stdout ?? "" };
}

async function emit(type: string, event: any) {
  await runOnibi(["--agent", ONIBI_AGENT, "--format", "amp", "--type", type], event).catch(() => {});
}

async function approval(event: any) {
  const payload = {
    hook_event_name: "tool.call",
    session_id: event?.thread?.id ?? event?.sessionId ?? event?.session_id ?? event?.session?.id,
    provider_session_id: event?.thread?.id ?? event?.sessionId ?? event?.session_id ?? event?.session?.id,
    cwd: event?.cwd ?? event?.directory,
    tool_name: event?.toolName ?? event?.tool_name ?? event?.tool ?? event?.name ?? "tool.call",
    tool_input: event?.input ?? event?.args ?? {},
    raw: event
  };
  const r = await runOnibi(["--agent", ONIBI_AGENT, "--format", "amp", "--type", "approval_request", "--wait", "--response", "onibi-json"], payload);
  if (!r.stdout.trim()) return { action: "allow" };
  const decision = JSON.parse(r.stdout);
  if (decision.decision === "deny" || decision.decision === "expired") {
    return { action: "reject-and-continue", message: decision.reason || "Denied by Onibi" };
  }
  if (decision.decision === "edited" && decision.updated_input) {
    return { action: "modify", input: parseUpdatedInput(decision.updated_input) };
  }
  return { action: "allow" };
}

function parseUpdatedInput(value: string) {
  const parsed = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Onibi edited input must be a JSON object");
  return parsed;
}

export default function (amp: PluginAPI) {
  amp.on("session.start", async (event: any) => emit("agent_message", event));
  amp.on("agent.start", async (event: any) => emit("agent_message", event));
  amp.on("tool.call", async (event: any) => {
    await emit("agent_message", event);
    return await approval(event);
  });
  amp.on("tool.result", async (event: any) => emit("agent_message", event));
  amp.on("agent.end", async (event: any) => emit("agent_done", event));
}
`, common.IntegrationVersion, notifyBin)
}
