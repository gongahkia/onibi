import { mkdtemp, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { installClaudeCodeHooks, normalizeClaudeCodeHook, redactJson } from "../src/index.js";

describe("agent hooks", () => {
  it("normalizes and redacts Claude Code hook payloads", () => {
    const event = normalizeClaudeCodeHook(
      {
        session_id: "session-1",
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_input: {
          command: "echo user@example.com && echo sk-testtesttesttesttest",
          env: { GITHUB_TOKEN: "ghp_aaaaaaaaaaaaaaaaaaaa" }
        },
        tool_response: { ok: true }
      },
      {}
    );

    expect(event).toMatchObject({
      sourceAgent: "claude-code",
      sessionId: "session-1",
      hookEvent: "PostToolUse",
      toolName: "Bash",
      status: "succeeded",
      classification: "Restricted"
    });
    expect(JSON.stringify(event.args)).toContain("[REDACTED_EMAIL]");
    expect(JSON.stringify(event.args)).toContain("[REDACTED_SECRET]");
  });

  it("redacts cards and singapore nric values in nested json", () => {
    expect(
      redactJson({
        text: "4111 1111 1111 1111 S1234567D"
      })
    ).toEqual({
      text: "[REDACTED_CARD] [REDACTED_NRIC]"
    });
  });

  it("installs Claude Code hook entries without duplicating commands", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-hooks-"));
    const settingsPath = join(tempDir, "settings.local.json");
    try {
      await installClaudeCodeHooks({
        settingsPath,
        command: "kelp-agent-hook send-event",
        events: ["PreToolUse", "SessionEnd"]
      });
      await installClaudeCodeHooks({
        settingsPath,
        command: "kelp-agent-hook send-event",
        events: ["PreToolUse", "SessionEnd"]
      });
      const settings = JSON.parse(await readFile(settingsPath, "utf8"));

      expect(settings.hooks.PreToolUse).toHaveLength(1);
      expect(settings.hooks.PreToolUse[0]).toMatchObject({
        matcher: "*",
        hooks: [{ type: "command", command: "kelp-agent-hook send-event" }]
      });
      expect(settings.hooks.SessionEnd).toHaveLength(1);
      expect(settings.hooks.SessionEnd[0]).toMatchObject({
        hooks: [{ type: "command", command: "kelp-agent-hook send-event" }]
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});
