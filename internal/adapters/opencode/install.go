package opencode

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

const Agent = "opencode"

func PluginPath() (string, error) {
	return common.HomePath("ONIBI_OPENCODE_PLUGIN", ".config", "opencode", "plugins", "onibi.js")
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
			info.Message = "not installed"
			return info
		}
		info.Message = err.Error()
		return info
	}
	version := installedVersion(string(body))
	info.Installed = strings.Contains(string(body), "ONIBI_AGENT = \"opencode\"")
	info.InstalledVersion = common.VersionPtr(version)
	info.Outdated = version != "" && version != common.IntegrationVersion
	if err := common.VerifyRecorded(ctx, db, Agent, path, body); err != nil {
		info.Message = err.Error()
	} else {
		info.Message = "OpenCode plugin installed"
	}
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
		return errors.New("Onibi-managed OpenCode plugin is missing")
	}
	return common.VerifyRecorded(ctx, db, Agent, path, body)
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

func pluginSource(notifyBin string) string {
	return fmt.Sprintf(`const ONIBI_INTEGRATION_VERSION = "%s";
const ONIBI_AGENT = "opencode";
const ONIBI_NOTIFY = %q;

async function runOnibi(args, payload) {
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
  if (decision.decision === "edited" && decision.updated_input) output.args = JSON.parse(decision.updated_input);
}

export const Onibi = async () => ({
  event: async (input) => emit("agent_message", input?.event ?? input),
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
