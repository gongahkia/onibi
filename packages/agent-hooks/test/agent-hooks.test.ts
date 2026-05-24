import { mkdtemp, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installClaudeCodeHooks,
  normalizeClaudeCodeHook,
  redactJson,
  smokeClaudeCodeHookEvents
} from "../src/index.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("agent hooks", () => {
  it("normalizes and redacts Claude Code hook payloads", () => {
    const event = normalizeClaudeCodeHook(
      {
        session_id: "session-1",
        hook_event_name: "PostToolUse",
        tool_name: "Bash",
        tool_input: {
          command: "echo user@example.com && echo sk-testtesttesttesttest",
          env: { GITHUB_TOKEN: "GITHUB_TOKEN_REDACTED" }
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

  it("smokes Claude Code PreToolUse and PostToolUse hook posts", async () => {
    const bodies: unknown[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: unknown, init: unknown) => {
        bodies.push(JSON.parse(String((init as { readonly body?: unknown }).body)));
        return new Response(
          JSON.stringify({
            ok: true,
            event: {
              id: `agent-step.${bodies.length}`,
              hookEvent: (bodies.at(-1) as { readonly hookEvent?: string }).hookEvent
            }
          }),
          { status: 201, headers: { "content-type": "application/json" } }
        );
      })
    );

    const smoke = await smokeClaudeCodeHookEvents({
      runId: "agent-run.smoke",
      apiBaseUrl: "http://127.0.0.1:8787",
      apiToken: "operator-token"
    });

    expect(smoke.events.map((event) => event.hookEvent)).toEqual(["PreToolUse", "PostToolUse"]);
    expect(bodies).toEqual([
      expect.objectContaining({
        sourceAgent: "claude-code",
        hookEvent: "PreToolUse",
        status: "pending"
      }),
      expect.objectContaining({
        sourceAgent: "claude-code",
        hookEvent: "PostToolUse",
        status: "succeeded"
      })
    ]);
  });
});
