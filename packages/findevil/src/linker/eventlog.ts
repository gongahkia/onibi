import { readFileSync } from "node:fs";
import {
  loadCuratedRuleset,
  matchEventLogAgainstSigma,
  sigmaMatchesAsEvidence,
  type SigmaRule
} from "../sigma/index.js";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface EventLogRecord {
  readonly artifact: string;
  readonly eventId: number;
  readonly channel: string;
  readonly recordId: string;
  readonly provider?: string | undefined;
  readonly timeCreated?: string | undefined;
  readonly message?: string | undefined;
  readonly eventData: Readonly<Record<string, string>>;
  readonly raw: unknown;
}

type JsonObject = Record<string, unknown>;

const persistenceEventEvidence = new Map<number, string>([
  [4698, "security_4698_scheduled_task"],
  [4702, "security_4702_scheduled_task"],
  [7045, "system_7045_service_create"]
]);
const genericClaimWords = new Set([
  "account",
  "channel",
  "created",
  "creation",
  "event",
  "failed",
  "installed",
  "logon",
  "process",
  "programdata",
  "public",
  "record",
  "scheduled",
  "security",
  "service",
  "successful",
  "system",
  "system32",
  "users",
  "windows"
]);
let curatedSigmaRules: SigmaRule[] | undefined;

export function parseEvtxJson(file: string, artifact = file): EventLogRecord[] {
  const input = readFileSync(file, "utf8");
  return parseJsonRecords(input)
    .map((record, index) => normalizeEventLogRecord(record, index, artifact))
    .filter((record): record is EventLogRecord => record !== undefined);
}

export function matchEventLogProcessCreate(
  claim: Pick<Claim, "text">,
  records: readonly EventLogRecord[]
): EvidenceRef[] {
  return withRequestedSigmaEvidence(
    claim,
    records,
    records
      .filter((record) => record.eventId === 4688 && isChannel(record, "Security"))
      .filter((record) => recordMatchesClaim(claim.text, record))
      .map((record) => eventLogRecordToEvidenceRef(record, "security_4688_process_create"))
  );
}

export function matchEventLogLogon(
  claim: Pick<Claim, "text">,
  records: readonly EventLogRecord[]
): EvidenceRef[] {
  return withRequestedSigmaEvidence(
    claim,
    records,
    records
      .filter(
        (record) =>
          (record.eventId === 4624 || record.eventId === 4625) && isChannel(record, "Security")
      )
      .filter((record) => recordMatchesClaim(claim.text, record))
      .map((record) => eventLogRecordToEvidenceRef(record, logonSupport(record)))
  );
}

export function matchEventLogServiceInstall(
  claim: Pick<Claim, "text">,
  records: readonly EventLogRecord[]
): EvidenceRef[] {
  return withRequestedSigmaEvidence(
    claim,
    records,
    records
      .filter((record) => record.eventId === 7045 && isChannel(record, "System"))
      .filter((record) => recordMatchesClaim(claim.text, record))
      .map((record) =>
        eventLogRecordToEvidenceRef(record, persistenceEventEvidence.get(7045) ?? "service-create")
      )
  );
}

export function matchEventLogScheduledTask(
  claim: Pick<Claim, "text">,
  records: readonly EventLogRecord[]
): EvidenceRef[] {
  return withRequestedSigmaEvidence(
    claim,
    records,
    records
      .filter(
        (record) =>
          (record.eventId === 4698 || record.eventId === 4702) && isChannel(record, "Security")
      )
      .filter((record) => recordMatchesClaim(claim.text, record))
      .map((record) =>
        eventLogRecordToEvidenceRef(
          record,
          persistenceEventEvidence.get(record.eventId) ?? "scheduled-task"
        )
      )
  );
}

function parseJsonRecords(input: string): unknown[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return recordsFromParsedJson(JSON.parse(trimmed) as unknown);
  } catch {
    return trimmed
      .split(/\r?\n/u)
      .filter((line) => line.trim().length > 0)
      .flatMap((line) => recordsFromParsedJson(JSON.parse(line) as unknown));
  }
}

function recordsFromParsedJson(parsed: unknown): unknown[] {
  if (Array.isArray(parsed)) {
    return parsed;
  }
  const object = asObject(parsed);
  if (!object) {
    return [];
  }
  for (const key of ["records", "Records", "events", "Events", "items", "Items", "data", "Data"]) {
    const value = object[key];
    if (Array.isArray(value)) {
      return value;
    }
  }
  return [parsed];
}

function normalizeEventLogRecord(
  raw: unknown,
  index: number,
  artifact: string
): EventLogRecord | undefined {
  const system = firstObjectAt(raw, [["Event", "System"], ["System"], ["system"]]);
  const eventId = numberValue(
    firstDefined([
      field(system, "EventID"),
      field(raw, "EventID"),
      field(raw, "event_id"),
      field(raw, "EventId")
    ])
  );
  if (eventId === undefined) {
    return undefined;
  }
  const channel = stringValue(
    firstDefined([field(system, "Channel"), field(raw, "Channel"), field(raw, "channel")])
  );
  const recordId = stringValue(
    firstDefined([
      field(system, "EventRecordID"),
      field(system, "EventRecordId"),
      field(raw, "EventRecordID"),
      field(raw, "EventRecordId"),
      field(raw, "RecordID"),
      field(raw, "RecordId"),
      field(raw, "record_id")
    ])
  );
  const providerValue = firstDefined([
    field(system, "Provider"),
    field(raw, "Provider"),
    field(raw, "provider")
  ]);
  const timeValue = firstDefined([
    field(system, "TimeCreated"),
    field(raw, "TimeCreated"),
    field(raw, "time_created"),
    field(raw, "timestamp")
  ]);
  const messageValue = firstDefined([
    field(raw, "Message"),
    field(raw, "message"),
    field(raw, "RenderedMessage")
  ]);

  const provider = stringValue(providerValue);
  const timeCreated = timeCreatedValue(timeValue);
  const message = stringValue(messageValue);
  return {
    artifact,
    eventId,
    channel: channel ?? "Unknown",
    recordId: recordId ?? String(index + 1),
    ...(provider ? { provider } : {}),
    ...(timeCreated ? { timeCreated } : {}),
    ...(message ? { message } : {}),
    eventData: eventData(raw),
    raw
  };
}

function eventData(raw: unknown): Readonly<Record<string, string>> {
  const values: Record<string, string> = {};
  for (const path of [
    ["Event", "EventData"],
    ["EventData"],
    ["event_data"],
    ["Event", "UserData"],
    ["UserData"]
  ]) {
    Object.assign(values, flattenEventData(valueAt(raw, path)));
  }
  Object.assign(values, flattenTopLevelScalars(raw));
  return values;
}

function flattenEventData(input: unknown): Record<string, string> {
  if (input === undefined) {
    return {};
  }
  if (Array.isArray(input)) {
    return Object.assign({}, ...input.map((item) => flattenEventData(item))) as Record<
      string,
      string
    >;
  }
  const object = asObject(input);
  if (!object) {
    return {};
  }
  const data = object.Data;
  if (Array.isArray(data) || asObject(data)) {
    return flattenEventData(data);
  }
  const name = stringValue(firstDefined([object.Name, object["@Name"], asObject(object.$)?.Name]));
  const namedValue = stringValue(
    firstDefined([object["#text"], object._, object.Value, object.value, object.Text, object.text])
  );
  if (name && namedValue) {
    return { [name]: namedValue };
  }
  const values: Record<string, string> = {};
  for (const [key, value] of Object.entries(object)) {
    if (key === "$") {
      continue;
    }
    const scalar = stringValue(value);
    if (scalar) {
      values[key] = scalar;
      continue;
    }
    Object.assign(values, flattenEventData(value));
  }
  return values;
}

function flattenTopLevelScalars(raw: unknown): Record<string, string> {
  const object = asObject(raw);
  if (!object) {
    return {};
  }
  const values: Record<string, string> = {};
  for (const [key, value] of Object.entries(object)) {
    if (["Event", "System", "EventData", "UserData"].includes(key)) {
      continue;
    }
    const scalar = stringValue(value);
    if (scalar) {
      values[key] = scalar;
    }
  }
  return values;
}

function eventLogRecordToEvidenceRef(record: EventLogRecord, supports: string): EvidenceRef {
  return {
    artifact: record.artifact,
    locator: `evtx:channel=${record.channel}:record=${record.recordId}`,
    supports,
    hash: hashEvidenceRow({
      eventId: record.eventId,
      channel: record.channel,
      recordId: record.recordId,
      eventData: record.eventData,
      raw: record.raw
    })
  };
}

function withRequestedSigmaEvidence(
  claim: Pick<Claim, "text">,
  records: readonly EventLogRecord[],
  refs: EvidenceRef[]
): EvidenceRef[] {
  if (!claimRequestsSigmaEvidence(claim)) {
    return refs;
  }
  curatedSigmaRules ??= loadCuratedRuleset();
  return [
    ...refs,
    ...sigmaMatchesAsEvidence(matchEventLogAgainstSigma(records, curatedSigmaRules))
  ];
}

function claimRequestsSigmaEvidence(claim: Pick<Claim, "text">): boolean {
  const missingEvidence = (claim as { readonly missingEvidence?: unknown }).missingEvidence;
  return Array.isArray(missingEvidence) && missingEvidence.includes("sigma_rule_match");
}

function logonSupport(record: EventLogRecord): string {
  if (record.eventId === 4625) {
    return "security_4625_logon";
  }
  const logonType = eventField(record, ["LogonType", "Logon Type"]);
  return logonType && /^\d+$/u.test(logonType)
    ? `security_4624_type_${logonType}`
    : "security_4624_logon";
}

function recordMatchesClaim(claimText: string, record: EventLogRecord): boolean {
  const terms = claimSearchTerms(claimText);
  if (terms.length === 0) {
    return false;
  }
  const haystack = normalizedSearchText([
    record.channel,
    record.provider,
    record.timeCreated,
    record.message,
    ...Object.entries(record.eventData).flat(),
    JSON.stringify(record.raw)
  ]);
  return terms.some((term) => haystack.includes(term));
}

function claimSearchTerms(text: string): string[] {
  const lower = normalizedSearchText([text]);
  const terms = new Set<string>();
  for (const match of lower.matchAll(/[a-z]:[\\/][^\s"'`]+/giu)) {
    terms.add(stripTrailingPunctuation(match[0]));
  }
  for (const match of lower.matchAll(
    /[a-z0-9_.-]+\.(?:exe|dll|ps1|bat|cmd|vbs|js|msi|scr|sys|com)\b/giu
  )) {
    terms.add(match[0]);
  }
  for (const match of lower.matchAll(/\b[a-z0-9._-]+\\[a-z0-9._$-]+\b/giu)) {
    terms.add(match[0]);
  }
  for (const match of lower.matchAll(/\b(?:\d{1,3}\.){3}\d{1,3}\b/gu)) {
    terms.add(match[0]);
  }
  for (const match of lower.matchAll(/["']([^"']{4,})["']/gu)) {
    const value = stripTrailingPunctuation(match[1]?.trim() ?? "");
    if (isUsefulWord(value)) {
      terms.add(value);
    }
  }
  for (const match of lower.matchAll(/\b[a-z0-9][a-z0-9._-]{4,}\b/gu)) {
    if (isUsefulWord(match[0])) {
      terms.add(match[0]);
    }
  }
  return [...terms].filter((term) => term.length >= 4);
}

function isUsefulWord(value: string): boolean {
  return !genericClaimWords.has(value);
}

function eventField(record: EventLogRecord, names: readonly string[]): string | undefined {
  const wanted = new Set(names.map(normalizeFieldName));
  for (const [key, value] of Object.entries(record.eventData)) {
    if (wanted.has(normalizeFieldName(key))) {
      return value;
    }
  }
  return undefined;
}

function isChannel(record: EventLogRecord, channel: string): boolean {
  return record.channel.toLowerCase() === channel.toLowerCase();
}

function normalizedSearchText(values: readonly unknown[]): string {
  return values
    .filter((value) => value !== undefined && value !== null)
    .join("\n")
    .toLowerCase()
    .replace(/\//gu, "\\");
}

function stripTrailingPunctuation(value: string): string {
  return value.replace(/[),.;:]+$/u, "");
}

function timeCreatedValue(input: unknown): string | undefined {
  const object = asObject(input);
  return stringValue(
    object?.SystemTime ?? object?.["@SystemTime"] ?? asObject(object?.$)?.SystemTime ?? input
  );
}

function firstObjectAt(
  input: unknown,
  paths: readonly (readonly string[])[]
): JsonObject | undefined {
  for (const path of paths) {
    const object = asObject(valueAt(input, path));
    if (object) {
      return object;
    }
  }
  return undefined;
}

function valueAt(input: unknown, path: readonly string[]): unknown {
  let current = input;
  for (const segment of path) {
    const object = asObject(current);
    if (!object) {
      return undefined;
    }
    current = field(object, segment);
  }
  return current;
}

function field(input: unknown, name: string): unknown {
  const object = asObject(input);
  if (!object) {
    return undefined;
  }
  if (name in object) {
    return object[name];
  }
  const normalized = normalizeFieldName(name);
  return Object.entries(object).find(([key]) => normalizeFieldName(key) === normalized)?.[1];
}

function firstDefined(values: readonly unknown[]): unknown {
  return values.find((value) => value !== undefined && value !== null && stringValue(value) !== "");
}

function numberValue(input: unknown): number | undefined {
  const value = stringValue(input);
  if (!value) {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function stringValue(input: unknown): string | undefined {
  if (typeof input === "string") {
    return input.trim();
  }
  if (typeof input === "number" || typeof input === "boolean") {
    return String(input);
  }
  const object = asObject(input);
  if (!object) {
    return undefined;
  }
  return stringValue(
    firstDefined([object["#text"], object._, object.Value, object.value, object.Name])
  );
}

function asObject(input: unknown): JsonObject | undefined {
  return input !== null && typeof input === "object" && !Array.isArray(input)
    ? (input as JsonObject)
    : undefined;
}

function normalizeFieldName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "");
}
