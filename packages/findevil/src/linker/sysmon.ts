import { existsSync, readFileSync } from "node:fs";
import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export type SysmonEventId = 1 | 3 | 11 | 13;

export interface SysmonEvent {
  readonly eventId: SysmonEventId;
  readonly recordNumber: number;
  readonly sourceLocator: string;
  readonly artifact: string;
  readonly data: Record<string, string>;
  readonly raw: unknown;
}

type JsonObject = Record<string, unknown>;

const supportedEventIds = new Set<number>([1, 3, 11, 13]);

export function parseSysmonJson(file: string, artifact?: string): SysmonEvent[] {
  const input = sysmonInput(file, artifact);
  return parseJsonRecords(input.contents).flatMap((record, index) =>
    normalizeSysmonRecord(record, index + 1, input.artifact)
  );
}

export function matchSysmonProcessCreate(
  claim: { readonly text: string },
  events: readonly SysmonEvent[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  return events
    .filter((event) => event.eventId === 1 && processCreateMatches(claimText, event))
    .map((event) => sysmonEventToEvidenceRef(event, "sysmon_process_create"));
}

export function matchSysmonNetworkConnect(
  claim: { readonly text: string },
  events: readonly SysmonEvent[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  return events
    .filter((event) => event.eventId === 3 && networkConnectMatches(claimText, event))
    .map((event) => sysmonEventToEvidenceRef(event, "sysmon_network_connect"));
}

function parseJsonRecords(input: string): JsonObject[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return collectRecords(JSON.parse(trimmed));
  } catch {
    return trimmed.split(/\r?\n/u).flatMap((line) => {
      const candidate = line.trim();
      if (candidate.length === 0) {
        return [];
      }
      try {
        return collectRecords(JSON.parse(candidate));
      } catch {
        return [];
      }
    });
  }
}

function sysmonInput(
  file: string,
  artifact: string | undefined
): { contents: string; artifact: string } {
  const trimmed = file.trimStart();
  if (
    !trimmed.startsWith("{") &&
    !trimmed.startsWith("[") &&
    !file.includes("\n") &&
    existsSync(file)
  ) {
    return { contents: readFileSync(file, "utf8"), artifact: artifact ?? file };
  }
  return { contents: file, artifact: artifact ?? "sysmon.json" };
}

function collectRecords(value: unknown): JsonObject[] {
  if (Array.isArray(value)) {
    return value.filter(isObject);
  }
  if (!isObject(value)) {
    return [];
  }
  for (const key of ["events", "Events", "records", "Records", "items", "Items"]) {
    const nested = value[key];
    if (Array.isArray(nested)) {
      return nested.filter(isObject);
    }
  }
  return [value];
}

function normalizeSysmonRecord(
  record: JsonObject,
  fallbackRecordNumber: number,
  artifact: string
): SysmonEvent[] {
  const event = firstObject(record.Event, record.event) ?? record;
  const system = firstObject(event.System, event.system) ?? event;
  const eventId = numberField(system, ["EventID", "EventId", "event_id", "eventId", "Id", "id"]);
  if (!eventId || !supportedEventIds.has(eventId)) {
    return [];
  }
  const recordNumber =
    numberField(system, [
      "EventRecordID",
      "EventRecordId",
      "RecordNumber",
      "RecordId",
      "record_id",
      "recordNumber"
    ]) ?? fallbackRecordNumber;
  return [
    {
      eventId: eventId as SysmonEventId,
      recordNumber,
      sourceLocator: `sysmon:eventid=${eventId}:record=${recordNumber}`,
      artifact,
      data: eventData(record, event),
      raw: record
    }
  ];
}

function eventData(record: JsonObject, event: JsonObject): Record<string, string> {
  const data: Record<string, string> = {};
  mergeScalarFields(data, record);
  mergeScalarFields(data, event);
  for (const source of [
    firstObject(record.EventData, record.eventData, record.event_data),
    firstObject(event.EventData, event.eventData, event.event_data)
  ]) {
    if (source) {
      mergeEventData(data, source);
    }
  }
  return data;
}

function mergeScalarFields(target: Record<string, string>, source: JsonObject): void {
  for (const [key, value] of Object.entries(source)) {
    if (
      ["Event", "event", "System", "system", "EventData", "eventData", "event_data"].includes(key)
    ) {
      continue;
    }
    const scalar = stringValue(value);
    if (scalar !== undefined) {
      target[key] = scalar;
    }
  }
}

function mergeEventData(target: Record<string, string>, source: JsonObject): void {
  for (const [key, value] of Object.entries(source)) {
    if (key === "Data" || key === "data") {
      mergeDataItems(target, value);
      continue;
    }
    const scalar = stringValue(value);
    if (scalar !== undefined) {
      target[key] = scalar;
    }
  }
}

function mergeDataItems(target: Record<string, string>, value: unknown): void {
  const values = Array.isArray(value) ? value : [value];
  for (const item of values) {
    if (!isObject(item)) {
      continue;
    }
    const name = stringField(item, ["Name", "name", "@Name"]);
    const content = stringField(item, ["#text", "_", "Value", "value", "Text", "text"]);
    if (name && content !== undefined) {
      target[name] = content;
    }
  }
}

function processCreateMatches(claimText: string, event: SysmonEvent): boolean {
  return imageTerms(event).some((term) => includesTerm(claimText, term));
}

function networkConnectMatches(claimText: string, event: SysmonEvent): boolean {
  const terms = [
    ...imageTerms(event),
    ...fieldValues(event, [
      "SourceIp",
      "SourceHostname",
      "DestinationIp",
      "DestinationHostname",
      "DestinationPortName"
    ])
  ].map((term) => term.toLowerCase());
  if (terms.some((term) => includesTerm(claimText, term))) {
    return true;
  }
  return fieldValues(event, ["SourcePort", "DestinationPort"]).some((port) =>
    includesPort(claimText, port)
  );
}

function imageTerms(event: SysmonEvent): string[] {
  const images = fieldValues(event, ["Image", "SourceImage", "ParentImage", "OriginalFileName"]);
  return uniqueTerms(images.flatMap((image) => [image, pathBasename(image)]));
}

function sysmonEventToEvidenceRef(event: SysmonEvent, supports: string): EvidenceRef {
  return {
    artifact: event.artifact,
    locator: event.sourceLocator,
    supports,
    hash: hashEvidenceRow(event)
  };
}

function fieldValues(event: SysmonEvent, names: readonly string[]): string[] {
  return names.flatMap((name) => {
    const value = caseInsensitiveValue(event.data, name);
    return value ? [value] : [];
  });
}

function caseInsensitiveValue(record: Record<string, string>, field: string): string | undefined {
  const direct = record[field];
  if (direct !== undefined) {
    return direct;
  }
  const lower = field.toLowerCase();
  const match = Object.entries(record).find(([key]) => key.toLowerCase() === lower);
  return match?.[1];
}

function includesTerm(text: string, term: string): boolean {
  const normalized = term.trim().toLowerCase();
  if (normalized.length < 3) {
    return false;
  }
  if (/[\\/:\s]/u.test(normalized)) {
    return text.includes(normalized);
  }
  return new RegExp(`(^|[^a-z0-9_.-])${escapeRegExp(normalized)}($|[^a-z0-9_.-])`, "u").test(text);
}

function includesPort(text: string, port: string): boolean {
  const normalized = port.trim();
  if (!/^\d{1,5}$/u.test(normalized)) {
    return false;
  }
  return new RegExp(`(^|[^0-9])${escapeRegExp(normalized)}([^0-9]|$)`, "u").test(text);
}

function uniqueTerms(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim().toLowerCase()).filter(Boolean))];
}

function pathBasename(path: string): string {
  return path.split(/[\\/]/u).at(-1) ?? path;
}

function firstObject(...values: readonly unknown[]): JsonObject | undefined {
  return values.find(isObject);
}

function numberField(source: JsonObject, names: readonly string[]): number | undefined {
  for (const name of names) {
    const value = field(source, name);
    const normalized = numberValue(value);
    if (normalized !== undefined) {
      return normalized;
    }
  }
  return undefined;
}

function stringField(source: JsonObject, names: readonly string[]): string | undefined {
  for (const name of names) {
    const value = stringValue(field(source, name));
    if (value !== undefined) {
      return value;
    }
  }
  return undefined;
}

function field(source: JsonObject, name: string): unknown {
  if (name in source) {
    return source[name];
  }
  const lower = name.toLowerCase();
  const match = Object.entries(source).find(([key]) => key.toLowerCase() === lower);
  return match?.[1];
}

function stringValue(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (isObject(value)) {
    return stringField(value, ["#text", "_", "Value", "value", "Text", "text"]);
  }
  return undefined;
}

function numberValue(value: unknown): number | undefined {
  const scalar = stringValue(value);
  if (scalar === undefined) {
    return undefined;
  }
  const number = Number(scalar);
  return Number.isFinite(number) ? number : undefined;
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}
