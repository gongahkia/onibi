#!/usr/bin/env node
import {
  claudeHookOutputForResult,
  installClaudeCodeHooks,
  sendClaudeCodeHookEventFromStdin,
  smokeClaudeCodeHookEvents
} from "./send-event.js";
import type { AgentStepSourceAgent } from "@kelpclaw/workflow-spec";

export {
  claudeHookOutputForResult,
  inferClassification,
  installClaudeCodeHooks,
  normalizeClaudeCodeHook,
  postHookEvent,
  redactJson,
  sendClaudeCodeHookEventFromStdin,
  smokeClaudeCodeHookEvents
} from "./send-event.js";

async function main(argv: readonly string[]): Promise<void> {
  const [command = "send-event", ...args] = argv;
  switch (command) {
    case "send-event": {
      const runId = requiredOption(args, "--run-id", process.env.KELPCLAW_AGENT_RUN_ID);
      const result = await sendClaudeCodeHookEventFromStdin({
        runId,
        apiBaseUrl: option(args, "--api-url") ?? process.env.KELPCLAW_API_URL,
        apiToken:
          option(args, "--token") ??
          process.env.KELPCLAW_API_TOKEN ??
          process.env.KELPCLAW_ADMIN_TOKEN,
        sourceAgent: sourceAgentOption(args)
      });
      const hookEvent =
        result.payload && typeof result.payload === "object" && "event" in result.payload
          ? String(
              (result.payload.event as { readonly hookEvent?: unknown } | undefined)?.hookEvent ??
                ""
            )
          : "";
      process.stdout.write(`${JSON.stringify(claudeHookOutputForResult(result, hookEvent))}\n`);
      if (!result.ok && result.statusCode !== 403) {
        process.exitCode = 1;
      }
      return;
    }
    case "install-claude-code": {
      const result = await installClaudeCodeHooks({
        settingsPath: option(args, "--settings"),
        command: option(args, "--command")
      });
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      return;
    }
    case "smoke-claude-code": {
      const runId = requiredOption(args, "--run-id", process.env.KELPCLAW_AGENT_RUN_ID);
      const result = await smokeClaudeCodeHookEvents({
        runId,
        apiBaseUrl: option(args, "--api-url") ?? process.env.KELPCLAW_API_URL,
        apiToken:
          option(args, "--token") ??
          process.env.KELPCLAW_API_TOKEN ??
          process.env.KELPCLAW_ADMIN_TOKEN,
        sourceAgent: sourceAgentOption(args)
      });
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      if (result.events.some((event) => !event.ok)) {
        process.exitCode = 1;
      }
      return;
    }
    default:
      throw new Error(
        "Usage: kelp-agent-hook <send-event|install-claude-code|smoke-claude-code> [--run-id id] [--settings path]"
      );
  }
}

function sourceAgentOption(args: readonly string[]): AgentStepSourceAgent | undefined {
  const value = option(args, "--agent");
  return value ? (value as AgentStepSourceAgent) : undefined;
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  const value = index >= 0 ? args[index + 1] : undefined;
  return value && !value.startsWith("--") ? value : undefined;
}

function requiredOption(args: readonly string[], name: string, fallback?: string): string {
  const value = option(args, name) ?? fallback;
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main(process.argv.slice(2)).catch((error: unknown) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
