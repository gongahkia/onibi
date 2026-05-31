import { readFileSync } from "node:fs";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export type RegistryHive = "SYSTEM" | "SOFTWARE" | "NTUSER";

export interface RegistryRecord {
  readonly artifact: string;
  readonly hive: RegistryHive;
  readonly key: string;
  readonly valueName: string;
  readonly valueData?: string | undefined;
  readonly valueType?: string | undefined;
  readonly timestamp?: string | undefined;
  readonly raw: unknown;
}

interface ValueDraft {
  readonly name?: string | undefined;
  readonly data?: string | undefined;
  readonly type?: string | undefined;
  readonly raw: unknown;
}

type JsonObject = Record<string, unknown>;

const genericClaimWords = new Set([
  "autostart",
  "created",
  "currentversion",
  "executed",
  "installed",
  "launches",
  "persisted",
  "registry",
  "runonce",
  "scheduled",
  "service",
  "software",
  "system",
  "task",
  "taskcache",
  "value",
  "windows"
]);

export function parseRegistryJson(file: string, artifact = file): RegistryRecord[] {
  const input = readFileSync(file, "utf8");
  return parseRegistryRecords(input).flatMap((record, index) =>
    normalizeRegistryRecord(record, index, artifact)
  );
}

export function matchRegistryRunKey(
  claim: Pick<Claim, "text">,
  records: readonly RegistryRecord[]
): EvidenceRef[] {
  return records
    .filter(isRunKeyRecord)
    .filter((record) => recordMatchesClaim(claim.text, record))
    .map((record) => registryRecordToEvidenceRef(record, "registry-run-key"));
}

export function matchRegistryService(
  claim: Pick<Claim, "text">,
  records: readonly RegistryRecord[]
): EvidenceRef[] {
  return records
    .filter(isServiceRecord)
    .filter((record) => recordMatchesClaim(claim.text, record))
    .map((record) => registryRecordToEvidenceRef(record, "registry-service"));
}

export function matchRegistryScheduledTask(
  claim: Pick<Claim, "text">,
  records: readonly RegistryRecord[]
): EvidenceRef[] {
  return records
    .filter(isScheduledTaskRecord)
    .filter((record) => recordMatchesClaim(claim.text, record))
    .map((record) => registryRecordToEvidenceRef(record, "scheduled-task"));
}

export function matchRegistryShimCache(
  claim: Pick<Claim, "text">,
  records: readonly RegistryRecord[]
): EvidenceRef[] {
  return records
    .filter(isShimCacheRecord)
    .filter((record) => recordMatchesClaim(claim.text, record))
    .map((record) => registryRecordToEvidenceRef(record, "shimcache_indicator"));
}

function parseRegistryRecords(input: string): unknown[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return collectRecords(JSON.parse(trimmed) as unknown);
  } catch {
    const jsonlRecords = trimmed
      .split(/\r?\n/u)
      .filter((line) => line.trim().length > 0)
      .flatMap((line) => {
        try {
          return collectRecords(JSON.parse(line) as unknown);
        } catch {
          return [];
        }
      });
    return jsonlRecords.length > 0 ? jsonlRecords : parseCsvRecords(trimmed);
  }
}

function collectRecords(parsed: unknown): unknown[] {
  if (Array.isArray(parsed)) {
    return parsed.flatMap(collectRecords);
  }
  const object = asObject(parsed);
  if (!object) {
    return [];
  }
  for (const key of ["records", "Records", "rows", "Rows", "items", "Items", "data", "Data"]) {
    const value = object[key];
    if (Array.isArray(value)) {
      return value.flatMap(collectRecords);
    }
  }
  return [parsed];
}

function normalizeRegistryRecord(
  raw: unknown,
  index: number,
  artifact: string
): RegistryRecord[] {
  const object = asObject(raw);
  if (!object) {
    return [];
  }
  const rawKey = firstStringField(object, [
    "key",
    "Key",
    "KeyPath",
    "Key Path",
    "key_path",
    "RegistryPath",
    "registry_path",
    "HivePath",
    "hive_path",
    "Path"
  ]);
  const hive = inferHive(object, rawKey, artifact);
  if (!hive || !rawKey) {
    return [];
  }
  const key = cleanRegistryKey(rawKey, hive);
  if (!key) {
    return [];
  }
  const values = valueDrafts(object);
  if (values.length > 0) {
    return values.map((value) =>
      registryRecordFromParts({
        artifact,
        hive,
        key,
        valueName: value.name ?? "(default)",
        valueData: value.data,
        valueType: value.type,
        timestamp: timestampFromObject(object),
        raw: { index, record: raw, value: value.raw }
      })
    );
  }
  return [
    registryRecordFromParts({
      artifact,
      hive,
      key,
      valueName: firstStringField(object, [
        "ValueName",
        "Value Name",
        "valueName",
        "value_name",
        "Name",
        "name"
      ]) ?? "(default)",
      valueData: valueDataFromObject(object),
      valueType: firstStringField(object, ["ValueType", "Value Type", "valueType", "value_type"]),
      timestamp: timestampFromObject(object),
      raw: { index, record: raw }
    })
  ];
}

function registryRecordFromParts(parts: RegistryRecord): RegistryRecord {
  return {
    artifact: parts.artifact,
    hive: parts.hive,
    key: parts.key,
    valueName: parts.valueName.trim() || "(default)",
    ...(parts.valueData ? { valueData: parts.valueData } : {}),
    ...(parts.valueType ? { valueType: parts.valueType } : {}),
    ...(parts.timestamp ? { timestamp: parts.timestamp } : {}),
    raw: parts.raw
  };
}

function valueDrafts(object: JsonObject): ValueDraft[] {
  const values = firstDefined([field(object, "Values"), field(object, "values")]);
  if (Array.isArray(values)) {
    return values.flatMap((value): ValueDraft[] => {
      const valueObject = asObject(value);
      if (!valueObject) {
        return [];
      }
      return [
        {
          raw: value,
          ...(firstStringField(valueObject, [
            "ValueName",
            "Value Name",
            "valueName",
            "value_name",
            "Name",
            "name"
          ])
            ? {
                name: firstStringField(valueObject, [
                  "ValueName",
                  "Value Name",
                  "valueName",
                  "value_name",
                  "Name",
                  "name"
                ])
              }
            : {}),
          ...(valueDataFromObject(valueObject) ? { data: valueDataFromObject(valueObject) } : {}),
          ...(firstStringField(valueObject, ["ValueType", "Value Type", "valueType", "value_type"])
            ? {
                type: firstStringField(valueObject, [
                  "ValueType",
                  "Value Type",
                  "valueType",
                  "value_type"
                ])
              }
            : {})
        }
      ];
    });
  }
  const valuesObject = asObject(values);
  if (!valuesObject) {
    return [];
  }
  return Object.entries(valuesObject).map(([name, data]) => ({
    name,
    ...(stringValue(data) ? { data: stringValue(data) } : {}),
    raw: { [name]: data }
  }));
}

function valueDataFromObject(object: JsonObject): string | undefined {
  const parts = [
    "ValueData",
    "Value Data",
    "valueData",
    "value_data",
    "ValueData1",
    "ValueData2",
    "ValueData3",
    "Data",
    "data",
    "Details",
    "details",
    "Command",
    "command",
    "ImagePath",
    "imagePath",
    "Target",
    "target",
    "Value",
    "value"
  ]
    .map((name) => stringValue(field(object, name)))
    .filter((value): value is string => value !== undefined && value.length > 0);
  return unique(parts).join(" ") || undefined;
}

function timestampFromObject(object: JsonObject): string | undefined {
  return firstStringField(object, [
    "LastWriteTimestamp",
    "LastWrite Time",
    "LastWriteTime",
    "LastWrite",
    "lastWrite",
    "LastModified",
    "lastModified",
    "Timestamp",
    "timestamp"
  ]);
}

function inferHive(
  object: JsonObject,
  rawKey: string | undefined,
  artifact: string
): RegistryHive | undefined {
  const candidates = [
    firstStringField(object, [
      "Hive",
      "hive",
      "HiveType",
      "Hive Type",
      "hiveType",
      "hive_type",
      "SourceHive",
      "sourceHive"
    ]),
    rawKey,
    artifact
  ];
  for (const candidate of candidates) {
    const hive = hiveFromText(candidate);
    if (hive) {
      return hive;
    }
  }
  return undefined;
}

function hiveFromText(value: string | undefined): RegistryHive | undefined {
  const lower = value?.toLowerCase() ?? "";
  if (/\b(?:ntuser|hkcu|hkey_current_user)\b/u.test(lower)) {
    return "NTUSER";
  }
  if (/\b(?:system|hklm\\system|hkey_local_machine\\system)\b/u.test(lower)) {
    return "SYSTEM";
  }
  if (/\b(?:software|hklm\\software|hkey_local_machine\\software)\b/u.test(lower)) {
    return "SOFTWARE";
  }
  return undefined;
}

function cleanRegistryKey(value: string, hive: RegistryHive): string | undefined {
  let key = value
    .trim()
    .replace(/^["']|["']$/gu, "")
    .replace(/\//gu, "\\")
    .replace(/\\+/gu, "\\")
    .replace(/^computer\\/iu, "")
    .replace(/^\\+/u, "");
  if (/^[a-z]:\\/iu.test(key)) {
    return undefined;
  }
  key = key
    .replace(/^hkey_local_machine\\/iu, "HKLM\\")
    .replace(/^hkey_current_user\\/iu, "HKCU\\")
    .replace(/^\\registry\\machine\\/iu, "HKLM\\")
    .replace(/^\\registry\\user\\[^\\]+\\/iu, "HKCU\\")
    .replace(/^hklm\\system\\/iu, "")
    .replace(/^hklm\\software\\/iu, "")
    .replace(/^hkcu\\/iu, "")
    .replace(/^ntuser\.dat\\/iu, "")
    .replace(/^usrclass\.dat\\/iu, "");
  if (hive === "SYSTEM") {
    key = key.replace(/^system\\/iu, "");
  }
  if (hive === "SOFTWARE") {
    key = key.replace(/^software\\/iu, "");
  }
  key = key.replace(/^\\+|\\+$/gu, "");
  return key.includes("\\") ? key : undefined;
}

function isRunKeyRecord(record: RegistryRecord): boolean {
  if (record.hive !== "SOFTWARE" && record.hive !== "NTUSER") {
    return false;
  }
  const key = normalizedRegistryKey(record.key);
  return key.includes("microsoft\\windows\\currentversion\\") && /\\run(?:once)?$/u.test(key);
}

function isServiceRecord(record: RegistryRecord): boolean {
  if (record.hive !== "SYSTEM") {
    return false;
  }
  return /(?:^|\\)(?:currentcontrolset|controlset\d{3})\\services\\[^\\]+(?:\\|$)/u.test(
    normalizedRegistryKey(record.key)
  );
}

function isScheduledTaskRecord(record: RegistryRecord): boolean {
  if (record.hive !== "SOFTWARE") {
    return false;
  }
  const key = normalizedRegistryKey(record.key);
  return (
    key.includes("microsoft\\windows nt\\currentversion\\schedule\\taskcache\\tasks") ||
    key.includes("microsoft\\windows nt\\schedule\\taskcache\\tasks") ||
    key.includes("microsoft\\windows nt\\currentversion\\schedule\\taskcache\\tree") ||
    key.includes("microsoft\\windows nt\\schedule\\taskcache\\tree")
  );
}

function isShimCacheRecord(record: RegistryRecord): boolean {
  if (record.hive !== "SYSTEM") {
    return false;
  }
  const key = normalizedRegistryKey(record.key);
  const valueName = record.valueName.toLowerCase();
  return (
    key.includes("appcompatcache") ||
    key.includes("appcompatibility") ||
    valueName === "appcompatcache"
  );
}

function registryRecordToEvidenceRef(record: RegistryRecord, supports: string): EvidenceRef {
  return {
    artifact: record.artifact,
    locator: `registry:hive=${record.hive}:key=${record.key}:value=${record.valueName}`,
    supports,
    hash: hashEvidenceRow(record)
  };
}

function recordMatchesClaim(claimText: string, record: RegistryRecord): boolean {
  const terms = claimSearchTerms(claimText);
  if (terms.length === 0) {
    return false;
  }
  const haystack = normalizedSearchText([
    record.hive,
    record.key,
    record.valueName,
    record.valueData,
    record.valueType,
    record.timestamp,
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

function parseCsvRecords(input: string): JsonObject[] {
  const [header, ...rows] = parseCsv(input);
  if (!header) {
    return [];
  }
  const normalizedHeaders = header.map(normalizeFieldName);
  if (
    !normalizedHeaders.some((headerName) =>
      ["keypath", "hivetype", "valuename", "valuedata"].includes(headerName)
    )
  ) {
    return [];
  }
  return rows.map((row) =>
    Object.fromEntries(header.map((name, index) => [name, row[index]?.trim() ?? ""]))
  );
}

function parseCsv(input: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let fieldValue = "";
  let inQuotes = false;
  for (let index = 0; index < input.length; index += 1) {
    const char = input[index] ?? "";
    const next = input[index + 1] ?? "";
    if (char === '"' && inQuotes && next === '"') {
      fieldValue += '"';
      index += 1;
      continue;
    }
    if (char === '"') {
      inQuotes = !inQuotes;
      continue;
    }
    if (char === "," && !inQuotes) {
      row.push(fieldValue);
      fieldValue = "";
      continue;
    }
    if ((char === "\n" || char === "\r") && !inQuotes) {
      if (char === "\r" && next === "\n") {
        index += 1;
      }
      row.push(fieldValue);
      rows.push(row);
      row = [];
      fieldValue = "";
      continue;
    }
    fieldValue += char;
  }
  if (fieldValue.length > 0 || row.length > 0) {
    row.push(fieldValue);
    rows.push(row);
  }
  return rows;
}

function firstStringField(object: JsonObject, names: readonly string[]): string | undefined {
  for (const name of names) {
    const value = stringValue(field(object, name));
    if (value) {
      return value;
    }
  }
  return undefined;
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

function stringValue(input: unknown): string | undefined {
  if (typeof input === "string") {
    return input.trim();
  }
  if (typeof input === "number" || typeof input === "boolean") {
    return String(input);
  }
  if (Array.isArray(input)) {
    return unique(input.map(stringValue).filter((value): value is string => Boolean(value))).join(
      " "
    );
  }
  const object = asObject(input);
  if (!object) {
    return undefined;
  }
  return stringValue(
    firstDefined([
      object["#text"],
      object._,
      object.Value,
      object.value,
      object.Data,
      object.data,
      object.Text,
      object.text,
      object.Name,
      object.name
    ])
  );
}

function asObject(input: unknown): JsonObject | undefined {
  return input !== null && typeof input === "object" && !Array.isArray(input)
    ? (input as JsonObject)
    : undefined;
}

function normalizedRegistryKey(value: string): string {
  return value.toLowerCase().replace(/\//gu, "\\").replace(/\\+/gu, "\\");
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

function isUsefulWord(value: string): boolean {
  return !genericClaimWords.has(value);
}

function unique(values: readonly string[]): string[] {
  return [...new Set(values)];
}

function normalizeFieldName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "");
}
