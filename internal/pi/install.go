package pi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExtensionPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("ONIBI_PI_EXTENSION")); path != "" {
		return filepath.Abs(path)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ONIBI_PI_SCOPE")), "project") {
		return filepath.Abs(filepath.Join(".pi", "extensions", "onibi.ts"))
	}
	if dir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); dir != "" {
		return filepath.Abs(filepath.Join(dir, "extensions", "onibi.ts"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "agent", "extensions", "onibi.ts"), nil
}
func Install(ctx context.Context, notify string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(notify) {
		return "", errors.New("notify path must be absolute")
	}
	if info, err := os.Stat(notify); err != nil || info.IsDir() {
		return "", fmt.Errorf("onibi-notify unavailable: %w", err)
	}
	path, err := ExtensionPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(extensionSource(notify)), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
func extensionSource(notify string) string {
	return fmt.Sprintf(`import { spawnSync } from "node:child_process";
import type { ExtensionAPI, ExtensionContext, ToolCallEvent } from "@earendil-works/pi-coding-agent";

const ONIBI_NOTIFY = %q;

function requestApproval(event: ToolCallEvent, ctx: ExtensionContext) {
  if (!process.env.ONIBI_SESSION_ID) return undefined;
  const payload = JSON.stringify({
    version: "onibi.pi.v1",
    tool_name: event.toolName,
    tool_input: event.input,
    cwd: ctx.sessionManager.getCwd(),
    pi_session_id: ctx.sessionManager.getSessionId(),
  });
  const result = spawnSync(ONIBI_NOTIFY, ["--agent", "pi", "--format", "pi", "--type", "approval_request", "--wait", "--response", "onibi-json"], { input: payload, encoding: "utf8", timeout: 305_000, maxBuffer: 8 * 1024 });
  if (!result.stdout?.trim()) return { block: true, reason: "Onibi approval unavailable" };
  let decision: { decision?: string; reason?: string };
  try { decision = JSON.parse(result.stdout); } catch { return { block: true, reason: "Invalid Onibi response" }; }
  if (decision.decision === "approve") return undefined;
  return { block: true, reason: decision.reason || "Denied by Onibi" };
}

export default function (pi: ExtensionAPI) {
  pi.on("tool_call", requestApproval);
}
`, notify)
}
