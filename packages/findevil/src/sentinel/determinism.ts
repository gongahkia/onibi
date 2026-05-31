import { createHash } from "node:crypto";
import { stableJsonStringify, type JsonValue } from "@kelpclaw/workflow-spec";

export interface DeterministicModeOptions {
  readonly deterministic?: boolean | undefined;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
}

export function assertDeterministicMode(opts: DeterministicModeOptions): void {
  if (opts.deterministic !== true) {
    return;
  }
  pinExtractorTemperature();
  if (isNonEmptyString(opts.siftCommand)) {
    throw new Error(
      "Deterministic sentinel mode requires --trace; live --sift-command is refused."
    );
  }
  if (!isNonEmptyString(opts.tracePath)) {
    throw new Error("Deterministic sentinel mode requires a replay trace.");
  }
}

export function pinExtractorTemperature(): void {
  process.env.KELP_FINDEVIL_EXTRACTOR_TEMPERATURE = "0";
}

export function refuseFreshLlmCallOnCacheMiss(): void {
  throw new Error(
    "Deterministic sentinel mode requires a cached claim extractor response; refusing fresh LLM call on cache miss."
  );
}

export function stableSortRecord<T>(record: T): T {
  return sortValue(record) as T;
}

export function computeClaimLedgerHash(ledger: unknown): string {
  const payload = stableJsonStringify(stableSortRecord(ledger) as JsonValue);
  return `sha256:${createHash("sha256").update(payload, "utf8").digest("hex")}`;
}

function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((entry) => sortValue(entry));
  }
  if (!isPlainRecord(value)) {
    return value;
  }
  return Object.fromEntries(
    Object.entries(value)
      .filter(([, entryValue]) => entryValue !== undefined)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entryValue]) => [key, sortValue(entryValue)])
  );
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const prototype = Object.getPrototypeOf(value) as unknown;
  return prototype === Object.prototype || prototype === null;
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}
