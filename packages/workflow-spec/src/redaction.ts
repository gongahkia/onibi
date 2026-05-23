import type { AgentStepMetadata, JsonRecord, JsonValue } from "./types.js";

export const redactedValue = "[REDACTED]" as const;

export interface RedactionOptions {
  readonly secretRefs?: readonly string[] | undefined;
  readonly secretKeys?: readonly string[] | undefined;
}

const sensitiveKeyPattern =
  /(^|[_\-.])(authorization|api[-_]?key|access[-_]?token|refresh[-_]?token|token|secret|password|credential|cookie)([_\-.]|$)/iu;
const sensitiveValuePatterns = [
  /^raw:/iu,
  /^bearer\s+/iu,
  /^sk-[a-z0-9_-]+/iu,
  /^gh[opsu]_[a-z0-9_]+/iu,
  /^xox[baprs]-/iu,
  /^anthropic-[a-z0-9_-]+/iu
];

export function redactJsonValue(value: JsonValue, options: RedactionOptions = {}): JsonValue {
  const secretRefs = new Set(options.secretRefs ?? []);

  return redactValue(value, {
    secretRefs,
    secretKeys: new Set(options.secretKeys ?? [])
  });
}

export function redactJsonRecord(record: JsonRecord, options: RedactionOptions = {}): JsonRecord {
  return redactJsonValue(record, options) as JsonRecord;
}

export function redactSecretString(value: string, options: RedactionOptions = {}): string {
  return shouldRedactString(value, new Set(options.secretRefs ?? [])) ? redactedValue : value;
}

export function redactAgentStepMetadata(
  metadata: AgentStepMetadata,
  options: RedactionOptions = {}
): AgentStepMetadata {
  return {
    ...metadata,
    args: redactJsonRecord(metadata.args, options),
    ...(metadata.result !== undefined ? { result: redactJsonValue(metadata.result, options) } : {})
  };
}

function redactValue(
  value: JsonValue,
  options: {
    readonly secretRefs: ReadonlySet<string>;
    readonly secretKeys: ReadonlySet<string>;
  },
  key = ""
): JsonValue {
  if (typeof value === "string") {
    return shouldRedactKey(key, options.secretKeys) || shouldRedactString(value, options.secretRefs)
      ? redactedValue
      : value;
  }

  if (Array.isArray(value)) {
    return value.map((entry) => redactValue(entry, options, key));
  }

  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([entryKey, entryValue]) => [
        entryKey,
        shouldRedactKey(entryKey, options.secretKeys)
          ? redactedValue
          : redactValue(entryValue, options, entryKey)
      ])
    );
  }

  return value;
}

function shouldRedactKey(key: string, secretKeys: ReadonlySet<string>): boolean {
  return secretKeys.has(key) || sensitiveKeyPattern.test(key);
}

function shouldRedactString(value: string, secretRefs: ReadonlySet<string>): boolean {
  return secretRefs.has(value) || sensitiveValuePatterns.some((pattern) => pattern.test(value));
}
