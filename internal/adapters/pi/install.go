package pi

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

const Agent = "pi"

func init() {
	catalog.MustRegister(catalog.BuiltinAgentManifest(Agent, catalog.Adapter{
		Name:              Agent,
		Install:           Install,
		Uninstall:         Uninstall,
		Status:            Status,
		Verify:            VerifyHash,
		Adopt:             Adopt,
		TrustInstructions: TrustInstructions,
		DetectPresence:    DetectPresence,
	}, map[string]string{"PreToolUse": "*"}))
}

func ExtensionPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ONIBI_PI_EXTENSION")); v != "" {
		return filepath.Abs(v)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ONIBI_PI_SCOPE")), "project") {
		return filepath.Abs(filepath.Join(".pi", "extensions", "onibi.ts"))
	}
	if d := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); d != "" {
		return filepath.Abs(filepath.Join(d, "extensions", "onibi.ts"))
	}
	return common.HomePath("ONIBI_PI_EXTENSION", ".pi", "agent", "extensions", "onibi.ts")
}

func DetectPresence() bool {
	if path, err := ExtensionPath(); err == nil && common.ParentOrPathExists(path) {
		return true
	}
	return common.HomeExists(".pi")
}

func Install(ctx context.Context, db *store.DB, notifyBin string) error {
	if !filepath.IsAbs(notifyBin) {
		return errors.New("notifyBin must be absolute")
	}
	if _, err := os.Stat(notifyBin); err != nil {
		return fmt.Errorf("notify binary missing: %w", err)
	}
	path, err := ExtensionPath()
	if err != nil {
		return err
	}
	if _, err := common.BackupOriginal(ctx, db, Agent, path); err != nil {
		return err
	}
	body := []byte(extensionSource(notifyBin))
	if err := common.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return common.Record(ctx, db, Agent, path, body)
}

func Uninstall(ctx context.Context, db *store.DB) error {
	path, err := ExtensionPath()
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
	path, err := ExtensionPath()
	if err != nil {
		return common.Info{Name: Agent, Support: "blocking", BundledVersion: common.IntegrationVersion, Message: err.Error()}
	}
	return statusFile(ctx, db, path)
}

func VerifyHash(ctx context.Context, db *store.DB) error {
	path, err := ExtensionPath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), `ONIBI_AGENT = "pi"`) {
		return errors.New("onibi-managed Pi extension is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
}

func statusFile(ctx context.Context, db *store.DB, path string) common.Info {
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
	info.Installed = strings.Contains(src, `ONIBI_AGENT = "pi"`)
	if !info.Installed {
		if strings.Contains(src, "onibi-notify") {
			info.Message = "unmanaged onibi-like hook; run onibi install-hooks --agent pi to adopt"
			info.Next = "onibi install-hooks --agent pi"
		} else {
			common.MarkNotInstalled(&info)
		}
		return info
	}
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != common.IntegrationVersion
	common.ApplyManagedStatus(ctx, db, &info, Agent, path, body, "Pi extension installed", "onibi install-hooks --agent pi")
	return info
}

func Adopt(ctx context.Context, db *store.DB) error {
	path, err := ExtensionPath()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(string(body), `ONIBI_AGENT = "pi"`) {
		return errors.New("onibi-managed Pi extension is missing")
	}
	return common.Record(ctx, db, Agent, path, body)
}

func TrustInstructions() []string {
	return []string{
		"Pi next step: run /reload so auto-discovered extensions are reloaded.",
		"Pi default path is ~/.pi/agent/extensions/onibi.ts; use ONIBI_PI_SCOPE=project for .pi/extensions/onibi.ts, or ONIBI_PI_EXTENSION for an explicit path.",
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

func extensionSource(notifyBin string) string {
	return fmt.Sprintf(`import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const ONIBI_INTEGRATION_VERSION = "%s";
const ONIBI_AGENT = "pi";
const ONIBI_NOTIFY = %q;

async function runOnibi(args: string[], payload: any) {
  const env = (globalThis as any).process?.env ?? {};
  if (!env.ONIBI_SESSION_ID) return { code: 0, stdout: "" };
  const body = JSON.stringify(payload ?? {});
  if ((globalThis as any).Bun?.spawn) {
    const p = (globalThis as any).Bun.spawn([ONIBI_NOTIFY, ...args], { stdin: "pipe", stdout: "pipe", stderr: "pipe" });
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

function withSession(event: any, ctx: any) {
  const payload = event && typeof event === "object" ? { ...event } : {};
  const providerSessionID = ctx?.sessionManager?.getSessionId?.();
  if (typeof providerSessionID === "string" && providerSessionID) payload.provider_session_id = providerSessionID;
  if (!payload.cwd && !payload.directory && ctx?.cwd) payload.cwd = ctx.cwd;
  return payload;
}

async function emit(type: string, event: any, ctx: any) {
  await runOnibi(["--agent", ONIBI_AGENT, "--format", "pi", "--type", type], withSession(event, ctx)).catch(() => {});
}

async function approval(event: any, ctx: any) {
  const payload = {
    hook_event_name: "tool_call",
    session_id: event?.sessionId ?? event?.session_id ?? event?.session?.id,
    cwd: event?.cwd ?? event?.directory,
    tool_name: event?.toolName ?? event?.tool_name ?? event?.tool ?? "tool_call",
    tool_input: event?.input ?? event?.args ?? {},
    provider_session_id: ctx?.sessionManager?.getSessionId?.(),
    raw: event
  };
  const r = await runOnibi(["--agent", ONIBI_AGENT, "--format", "pi", "--type", "approval_request", "--wait", "--response", "onibi-json"], payload);
  if (!r.stdout.trim()) return undefined;
  let decision: any;
  try {
    decision = JSON.parse(r.stdout);
  } catch {
    return { block: true, reason: "Invalid Onibi approval response" };
  }
  if (decision.decision === "deny" || decision.decision === "expired") return { block: true, reason: decision.reason || "Denied by Onibi" };
  if (decision.decision === "edited") {
    if (!decision.updated_input || !event?.input || typeof event.input !== "object") return { block: true, reason: "Invalid Onibi edited input" };
    try {
      const nextInput = parseUpdatedInput(decision.updated_input);
      Object.assign(event.input, nextInput); // Pi does no post-mutation validation; validate before mutation
    } catch {
      return { block: true, reason: "Invalid Onibi edited input" };
    }
  }
  return undefined;
}

function parseUpdatedInput(value: string) {
  const parsed = JSON.parse(value);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Onibi edited input must be a JSON object");
  return parsed;
}

export default function (pi: ExtensionAPI) {
  pi.on("session_start", async (event: any, ctx: any) => emit("agent_message", event, ctx));
  pi.on("agent_start", async (event: any, ctx: any) => emit("agent_message", event, ctx));
  pi.on("input", async (event: any, ctx: any) => emit("agent_message", event, ctx));
  pi.on("tool_call", async (event: any, ctx: any) => {
    await emit("agent_message", event, ctx);
    return await approval(event, ctx);
  });
  pi.on("tool_result", async (event: any, ctx: any) => emit("agent_message", event, ctx));
  pi.on("agent_end", async (event: any, ctx: any) => emit("agent_done", event, ctx));
  pi.on("session_shutdown", async (event: any, ctx: any) => emit("session_exited", event, ctx));
}
`, common.IntegrationVersion, notifyBin)
}
