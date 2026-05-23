import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import type {
  AgentStepClassification,
  AgentStepSourceAgent,
  AgentStepStatus,
  JsonRecord,
  JsonValue
} from "@kelpclaw/workflow-spec";

export interface KelpClawHookPostOptions {
  readonly apiBaseUrl?: string | undefined;
  readonly apiToken?: string | undefined;
  readonly runId: string;
  readonly sourceAgent?: AgentStepSourceAgent | undefined;
}

export interface ClaudeCodeHookInput {
  readonly session_id?: unknown;
  readonly hook_event_name?: unknown;
  readonly tool_name?: unknown;
  readonly tool_input?: unknown;
  readonly tool_response?: unknown;
  readonly tool_use_id?: unknown;
  readonly parent_tool_use_id?: unknown;
  readonly cwd?: unknown;
  readonly transcript_path?: unknown;
  readonly [key: string]: unknown;
}

export interface AgentHookEventBody {
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId?: string | undefined;
  readonly parentToolUseId?: string | undefined;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: AgentStepStatus;
  readonly classification?: AgentStepClassification | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
}

export interface HookPostResult {
  readonly statusCode: number;
  readonly ok: boolean;
  readonly payload: JsonValue;
}

export interface ClaudeHookInstallOptions {
  readonly settingsPath?: string | undefined;
  readonly command?: string | undefined;
  readonly events?: readonly string[] | undefined;
}

const defaultApiBaseUrl = "http://127.0.0.1:8787";
const defaultClaudeEvents = [
  "PreToolUse",
  "PostToolUse",
  "PostToolUseFailure",
  "PermissionRequest",
  "Notification",
  "UserPromptSubmit",
  "Stop",
  "SubagentStart",
  "SubagentStop",
  "PreCompact",
  "SessionStart",
  "SessionEnd"
] as const;
const toolMatcherEvents = new Set(["PreToolUse", "PostToolUse", "PostToolUseFailure"]);
const secretKeyPattern =
  /(?:^|[_-])(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|secret|password|passwd|token)(?:$|[_-])/iu;
const tokenPattern =
  /\b(?:bearer\s+)?(?:sk-[a-z0-9_-]{16,}|gh[pousr]_[a-z0-9_]{16,}|xox[baprs]-[a-z0-9-]{16,})\b/giu;
const emailPattern = /\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/giu;
const nricPattern = /\b[STFGM]\d{7}[A-Z]\b/giu;
const cardPattern = /\b(?:\d[ -]*?){13,19}\b/gu;

export async function sendClaudeCodeHookEventFromStdin(
  options: KelpClawHookPostOptions
): Promise<HookPostResult> {
  const input = await readStdinJson();
  return postHookEvent(normalizeClaudeCodeHook(input, options), options);
}

export function normalizeClaudeCodeHook(
  input: ClaudeCodeHookInput,
  options: Pick<KelpClawHookPostOptions, "sourceAgent">
): AgentHookEventBody {
  const rawArgs = isJsonRecord(input.tool_input)
    ? input.tool_input
    : {
        hookInput: coerceJsonValue(input)
      };
  const rawResult =
    input.tool_response === undefined ? undefined : coerceJsonValue(input.tool_response);
  const classification = inferClassification(rawArgs, rawResult);
  return {
    sourceAgent: options.sourceAgent ?? "claude-code",
    sessionId: stringValue(input.session_id) ?? "claude-code.session",
    hookEvent: stringValue(input.hook_event_name) ?? "PostToolUse",
    toolName: stringValue(input.tool_name) ?? "ClaudeCode",
    ...(stringValue(input.tool_use_id) ? { toolUseId: stringValue(input.tool_use_id) } : {}),
    ...(stringValue(input.parent_tool_use_id)
      ? { parentToolUseId: stringValue(input.parent_tool_use_id) }
      : {}),
    args: redactJson(rawArgs) as JsonRecord,
    ...(rawResult !== undefined ? { result: redactJson(rawResult) } : {}),
    status: statusFromHook(input),
    ...(classification ? { classification } : {}),
    startedAt: new Date().toISOString(),
    ...(input.tool_response !== undefined ? { finishedAt: new Date().toISOString() } : {})
  };
}

export async function postHookEvent(
  body: AgentHookEventBody,
  options: KelpClawHookPostOptions
): Promise<HookPostResult> {
  const response = await fetch(
    new URL(
      `/api/agent-runs/${encodeURIComponent(options.runId)}/events`,
      options.apiBaseUrl ?? defaultApiBaseUrl
    ),
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...(options.apiToken ? { authorization: `Bearer ${options.apiToken}` } : {})
      },
      body: JSON.stringify(body)
    }
  );
  const payload = (await response.json()) as JsonValue;
  return {
    statusCode: response.status,
    ok: response.ok,
    payload
  };
}

export function claudeHookOutputForResult(result: HookPostResult, hookEvent: string): JsonValue {
  const payload = isJsonRecord(result.payload) ? result.payload : {};
  const decision = isJsonRecord(payload.decision) ? payload.decision : undefined;
  const action = stringValue(decision?.action);
  const reason =
    stringValue(payload.message) ?? stringValue(decision?.reason) ?? "KelpClaw policy gate";
  if (hookEvent === "PreToolUse" && result.statusCode === 403) {
    return {
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "deny",
        permissionDecisionReason: reason
      },
      suppressOutput: true
    };
  }
  if (hookEvent === "PreToolUse" && result.statusCode === 202 && action === "require-approval") {
    return {
      hookSpecificOutput: {
        hookEventName: "PreToolUse",
        permissionDecision: "ask",
        permissionDecisionReason: reason
      },
      suppressOutput: true
    };
  }
  return {
    continue: true,
    suppressOutput: true
  };
}

export async function installClaudeCodeHooks(
  options: ClaudeHookInstallOptions = {}
): Promise<{ readonly settingsPath: string; readonly events: readonly string[] }> {
  const settingsPath = resolve(options.settingsPath ?? ".claude/settings.local.json");
  const command =
    options.command ?? 'node "$CLAUDE_PROJECT_DIR/packages/agent-hooks/dist/index.js" send-event';
  const existing = await readJsonFile(settingsPath);
  const settings = isJsonRecord(existing) ? { ...existing } : {};
  const hooks = isJsonRecord(settings.hooks) ? { ...settings.hooks } : {};
  const events = options.events ?? defaultClaudeEvents;
  for (const event of events) {
    const current = Array.isArray(hooks[event]) ? [...(hooks[event] as JsonValue[])] : [];
    const entry = claudeHookEntry(event, command);
    if (!current.some((candidate) => stableJson(candidate) === stableJson(entry))) {
      current.push(entry);
    }
    hooks[event] = current;
  }
  await mkdir(dirname(settingsPath), { recursive: true });
  await writeFile(settingsPath, `${JSON.stringify({ ...settings, hooks }, null, 2)}\n`, "utf8");
  return { settingsPath, events };
}

export function redactJson(value: JsonValue): JsonValue {
  if (typeof value === "string") {
    return redactString(value);
  }
  if (Array.isArray(value)) {
    return value.map((item) => redactJson(item));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        secretKeyPattern.test(key) ? "[REDACTED_SECRET]" : redactJson(item)
      ])
    );
  }
  return value;
}

export function inferClassification(
  ...values: readonly (JsonValue | undefined)[]
): AgentStepClassification | undefined {
  const text = values.map((value) => JSON.stringify(value ?? null)).join("\n");
  if (matches(tokenPattern, text) || matches(nricPattern, text) || cardPatternLikelyMatches(text)) {
    return "Restricted";
  }
  if (matches(emailPattern, text)) {
    return "Confidential";
  }
  return undefined;
}

async function readStdinJson(): Promise<ClaudeCodeHookInput> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(String(chunk), "utf8"));
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  if (!raw) {
    throw new Error("KelpClaw hook expected Claude Code JSON on stdin.");
  }
  const parsed = JSON.parse(raw) as unknown;
  if (!isJsonRecord(parsed)) {
    throw new Error("KelpClaw hook input must be a JSON object.");
  }
  return parsed as ClaudeCodeHookInput;
}

function statusFromHook(input: ClaudeCodeHookInput): AgentStepStatus {
  const hookEvent = stringValue(input.hook_event_name);
  if (hookEvent === "PreToolUse" || hookEvent === "PermissionRequest") {
    return "pending";
  }
  if (hookEvent === "PostToolUseFailure") {
    return "failed";
  }
  return "succeeded";
}

function claudeHookEntry(event: string, command: string): JsonRecord {
  const hooks = [{ type: "command", command }];
  return toolMatcherEvents.has(event)
    ? { matcher: "*", hooks }
    : {
        hooks
      };
}

async function readJsonFile(path: string): Promise<JsonValue | undefined> {
  try {
    return JSON.parse(await readFile(path, "utf8")) as JsonValue;
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") {
      return undefined;
    }
    throw error;
  }
}

function redactString(value: string): string {
  return value
    .replace(tokenPattern, "[REDACTED_TOKEN]")
    .replace(emailPattern, "[REDACTED_EMAIL]")
    .replace(nricPattern, "[REDACTED_NRIC]")
    .replace(cardPattern, (candidate) => (luhnMaybe(candidate) ? "[REDACTED_CARD]" : candidate));
}

function luhnMaybe(value: string): boolean {
  const digits = value.replace(/\D/gu, "");
  if (digits.length < 13 || digits.length > 19) {
    return false;
  }
  let sum = 0;
  let doubleDigit = false;
  for (let index = digits.length - 1; index >= 0; index -= 1) {
    let digit = Number(digits[index]);
    if (doubleDigit) {
      digit *= 2;
      if (digit > 9) {
        digit -= 9;
      }
    }
    sum += digit;
    doubleDigit = !doubleDigit;
  }
  return sum % 10 === 0;
}

function matches(pattern: RegExp, value: string): boolean {
  pattern.lastIndex = 0;
  return pattern.test(value);
}

function cardPatternLikelyMatches(value: string): boolean {
  cardPattern.lastIndex = 0;
  return [...value.matchAll(cardPattern)].some((match) => luhnMaybe(match[0]));
}

function coerceJsonValue(value: unknown): JsonValue {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => coerceJsonValue(item));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, coerceJsonValue(item)])
    );
  }
  return null;
}

function stringValue(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function isJsonRecord(input: unknown): input is JsonRecord {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function stableJson(input: unknown): string {
  return JSON.stringify(input);
}
