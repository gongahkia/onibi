import { existsSync, readFileSync } from "node:fs";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export type MftAttribute = "SI" | "FN";

export interface MftTimestamps {
  readonly created?: string | undefined;
  readonly modified?: string | undefined;
  readonly mftModified?: string | undefined;
  readonly accessed?: string | undefined;
}

export interface MftRecord {
  readonly artifact: string;
  readonly entryNumber: string;
  readonly fileName: string;
  readonly extension?: string | undefined;
  readonly parentPath?: string | undefined;
  readonly fullPath?: string | undefined;
  readonly inUse?: boolean | undefined;
  readonly si: MftTimestamps;
  readonly fn: MftTimestamps;
  readonly raw: unknown;
}

type JsonObject = Record<string, unknown>;
type TimestampKind = keyof MftTimestamps;

interface FlatField {
  readonly key: string;
  readonly normalized: string;
  readonly tokens: readonly string[];
  readonly value: string;
}

interface ClaimFileTerms {
  readonly paths: readonly string[];
  readonly names: readonly string[];
  readonly extensions: readonly string[];
}

const jsonRecordKeys = [
  "records",
  "Records",
  "entries",
  "Entries",
  "items",
  "Items",
  "data",
  "Data",
  "files",
  "Files",
  "mft",
  "MFT"
];

export function parseMftJson(file: string, artifact?: string): MftRecord[] {
  const input = mftInput(file, artifact);
  return parseJsonRecords(input.contents).flatMap((record, index) =>
    normalizeMftRecord(record, index + 1, input.artifact)
  );
}

export function matchMftFileCreate(
  claim: Pick<Claim, "text">,
  records: readonly MftRecord[]
): EvidenceRef[] {
  return matchMftByTimestamp(claim, records, "created", "mft-file-create");
}

export function matchMftFileModify(
  claim: Pick<Claim, "text">,
  records: readonly MftRecord[]
): EvidenceRef[] {
  return matchMftByTimestamp(claim, records, "modified", "mft-file-modify");
}

export function matchMftFileDelete(
  claim: Pick<Claim, "text">,
  records: readonly MftRecord[]
): EvidenceRef[] {
  const terms = claimFileTerms(claim.text);
  if (!hasClaimFileTerms(terms)) {
    return [];
  }
  return records
    .filter((record) => record.inUse === false && recordMatchesFileTerms(record, terms))
    .flatMap((record) =>
      attributesWithAnyTimestamp(record).map((attribute) =>
        mftRecordToEvidenceRef(record, attribute, "mft-file-delete")
      )
    );
}

function matchMftByTimestamp(
  claim: Pick<Claim, "text">,
  records: readonly MftRecord[],
  timestamp: "created" | "modified",
  supports: string
): EvidenceRef[] {
  const terms = claimFileTerms(claim.text);
  if (!hasClaimFileTerms(terms)) {
    return [];
  }
  return records
    .filter((record) => recordMatchesFileTerms(record, terms))
    .flatMap((record) =>
      attributesWithTimestamp(record, timestamp).map((attribute) =>
        mftRecordToEvidenceRef(record, attribute, supports)
      )
    );
}

function mftInput(
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
  return { contents: file, artifact: artifact ?? "mft.json" };
}

function parseJsonRecords(input: string): unknown[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return collectRecords(JSON.parse(trimmed) as unknown);
  } catch {
    return trimmed.split(/\r?\n/u).flatMap((line) => {
      const candidate = line.trim();
      if (candidate.length === 0) {
        return [];
      }
      try {
        return collectRecords(JSON.parse(candidate) as unknown);
      } catch {
        return [];
      }
    });
  }
}

function collectRecords(value: unknown): unknown[] {
  if (Array.isArray(value)) {
    return value;
  }
  const object = asObject(value);
  if (!object) {
    return [];
  }
  for (const key of jsonRecordKeys) {
    const nested = object[key];
    if (Array.isArray(nested)) {
      return nested;
    }
    const nestedObject = asObject(nested);
    if (Array.isArray(nestedObject?.$values)) {
      return nestedObject.$values;
    }
  }
  if (Array.isArray(object.$values)) {
    return object.$values;
  }
  return [value];
}

function normalizeMftRecord(
  raw: unknown,
  fallbackEntryNumber: number,
  artifact: string
): MftRecord[] {
  const object = asObject(raw);
  if (!object) {
    return [];
  }
  const fields = flattenScalarFields(object);
  const fullPath = stringField(fields, [
    "fullpath",
    "full_name",
    "fullname",
    "filepath",
    "file_path",
    "path"
  ]);
  const fileName =
    stringField(fields, ["filename", "file_name", "name", "basename"]) ??
    (fullPath ? pathBasename(fullPath) : undefined);
  if (!fileName) {
    return [];
  }
  const parentPath = stringField(fields, [
    "parentpath",
    "parent_path",
    "directory",
    "directoryname",
    "folder",
    "folderpath"
  ]);
  const extension = normalizeExtension(
    stringField(fields, ["extension", "fileextension", "file_extension"]) ??
      extensionFromName(fileName)
  );
  const entryNumber =
    stringField(fields, [
      "entrynumber",
      "entry_number",
      "mftentrynumber",
      "mft_entry_number",
      "recordnumber",
      "record_number",
      "record",
      "entry"
    ]) ?? String(fallbackEntryNumber);
  const inUse = inUseValue(fields);
  const record: MftRecord = {
    artifact,
    entryNumber,
    fileName,
    ...(extension ? { extension } : {}),
    ...(parentPath ? { parentPath } : {}),
    ...(fullPath ? { fullPath } : {}),
    ...(inUse !== undefined ? { inUse } : {}),
    si: timestampsForAttribute(fields, "SI"),
    fn: timestampsForAttribute(fields, "FN"),
    raw
  };
  return [record];
}

function timestampsForAttribute(
  fields: readonly FlatField[],
  attribute: MftAttribute
): MftTimestamps {
  return {
    ...timestampProperty("created", timestampValue(fields, attribute, "created")),
    ...timestampProperty("modified", timestampValue(fields, attribute, "modified")),
    ...timestampProperty("mftModified", timestampValue(fields, attribute, "mftModified")),
    ...timestampProperty("accessed", timestampValue(fields, attribute, "accessed"))
  };
}

function timestampProperty<K extends TimestampKind>(
  key: K,
  value: string | undefined
): Pick<MftTimestamps, K> | Record<string, never> {
  return value ? ({ [key]: value } as Pick<MftTimestamps, K>) : {};
}

function timestampValue(
  fields: readonly FlatField[],
  attribute: MftAttribute,
  kind: TimestampKind
): string | undefined {
  return fields.find((field) => timestampFieldMatches(field, attribute, kind))?.value;
}

function timestampFieldMatches(
  field: FlatField,
  attribute: MftAttribute,
  kind: TimestampKind
): boolean {
  return attributeMatches(field, attribute) && timestampKindMatches(field, kind);
}

function attributeMatches(field: FlatField, attribute: MftAttribute): boolean {
  if (attribute === "SI") {
    return (
      field.tokens.includes("si") ||
      field.normalized.includes("0x10") ||
      field.normalized.includes("standardinformation") ||
      field.normalized.includes("standardinfo") ||
      (field.normalized.startsWith("si") && !field.normalized.startsWith("size"))
    );
  }
  return (
    field.tokens.includes("fn") ||
    field.normalized.includes("0x30") ||
    field.normalized.includes("filename") ||
    field.normalized.startsWith("fn")
  );
}

function timestampKindMatches(field: FlatField, kind: TimestampKind): boolean {
  switch (kind) {
    case "created":
      return (
        field.normalized.includes("created") ||
        field.normalized.includes("creation") ||
        field.tokens.includes("ctime")
      );
    case "modified":
      return (
        !isMftMetadataModified(field.normalized) &&
        (field.normalized.includes("modified") ||
          field.normalized.includes("lastwrite") ||
          field.tokens.includes("mtime"))
      );
    case "mftModified":
      return (
        field.normalized.includes("mftmodified") ||
        field.normalized.includes("recordchanged") ||
        field.normalized.includes("entrymodified") ||
        field.normalized.includes("metadatamodified")
      );
    case "accessed":
      return field.normalized.includes("accessed") || field.normalized.includes("lastaccess");
    default:
      return false;
  }
}

function isMftMetadataModified(normalized: string): boolean {
  return (
    normalized.includes("mftmodified") ||
    normalized.includes("recordmodified") ||
    normalized.includes("entrymodified") ||
    normalized.includes("metadatamodified")
  );
}

function inUseValue(fields: readonly FlatField[]): boolean | undefined {
  const inUse = stringField(fields, ["inuse", "in_use", "active", "isactive"]);
  if (inUse !== undefined) {
    return booleanValue(inUse);
  }
  const deleted = stringField(fields, ["deleted", "isdeleted", "is_deleted"]);
  const deletedValue = deleted === undefined ? undefined : booleanValue(deleted);
  return deletedValue === undefined ? undefined : !deletedValue;
}

function matchTimestamp(
  record: MftRecord,
  attribute: MftAttribute,
  timestamp: TimestampKind
): string | undefined {
  return attribute === "SI" ? record.si[timestamp] : record.fn[timestamp];
}

function attributesWithTimestamp(
  record: MftRecord,
  timestamp: "created" | "modified"
): MftAttribute[] {
  return (["SI", "FN"] as const).filter((attribute) =>
    matchTimestamp(record, attribute, timestamp)
  );
}

function attributesWithAnyTimestamp(record: MftRecord): MftAttribute[] {
  return (["SI", "FN"] as const).filter((attribute) =>
    Object.values(attribute === "SI" ? record.si : record.fn).some(Boolean)
  );
}

function mftRecordToEvidenceRef(
  record: MftRecord,
  attribute: MftAttribute,
  supports: string
): EvidenceRef {
  return {
    artifact: record.artifact,
    locator: `mft:record=${record.entryNumber}:attr=${attribute}`,
    supports,
    hash: hashEvidenceRow({ ...record, attribute, supports })
  };
}

function recordMatchesFileTerms(record: MftRecord, terms: ClaimFileTerms): boolean {
  const recordPaths = recordPathValues(record);
  if (
    terms.paths.some((term) =>
      recordPaths.some((recordPath) => recordPath.endsWith(term) || recordPath.includes(term))
    )
  ) {
    return true;
  }
  const recordName = normalizeFileTerm(record.fileName);
  if (
    terms.names.some(
      (term) =>
        recordName === term ||
        recordPaths.some(
          (recordPath) => recordPath.endsWith(`\\${term}`) || recordPath.includes(term)
        )
    )
  ) {
    return true;
  }
  if (terms.paths.length > 0 || terms.names.length > 0) {
    return false;
  }
  const recordExtension = normalizeExtension(
    record.extension ?? extensionFromName(record.fileName)
  );
  return recordExtension
    ? terms.extensions.some((extension) => extension === recordExtension)
    : false;
}

function recordPathValues(record: MftRecord): string[] {
  return [
    record.fullPath,
    record.parentPath ? combinePath(record.parentPath, record.fileName) : undefined
  ].flatMap((value) => (value ? [normalizePathTerm(value)] : []));
}

function claimFileTerms(text: string): ClaimFileTerms {
  const paths = new Set<string>();
  const names = new Set<string>();
  const extensions = new Set<string>();
  const normalizedText = text.toLowerCase().replace(/\//gu, "\\");
  for (const match of normalizedText.matchAll(/[a-z]:\\[^\s"'`]+/giu)) {
    const path = normalizePathTerm(stripTrailingPunctuation(match[0]));
    paths.add(path);
    const name = pathBasename(path);
    if (name) {
      names.add(normalizeFileTerm(name));
      addExtension(extensions, extensionFromName(name));
    }
  }
  for (const match of normalizedText.matchAll(
    /\b[a-z0-9][a-z0-9._$()-]{0,120}\.[a-z0-9]{1,10}\b/giu
  )) {
    const name = normalizeFileTerm(stripTrailingPunctuation(match[0]));
    if (looksLikeFileName(name)) {
      names.add(name);
      addExtension(extensions, extensionFromName(name));
    }
  }
  for (const match of normalizedText.matchAll(/\bextension\s+\.?([a-z0-9]{1,10})\b/giu)) {
    addExtension(extensions, match[1]);
  }
  for (const match of normalizedText.matchAll(/\*\.([a-z0-9]{1,10})\b/giu)) {
    addExtension(extensions, match[1]);
  }
  return {
    paths: [...paths].filter((term) => term.length >= 4),
    names: [...names].filter((term) => term.length >= 3),
    extensions: [...extensions].filter((term) => term.length > 0)
  };
}

function hasClaimFileTerms(terms: ClaimFileTerms): boolean {
  return terms.paths.length > 0 || terms.names.length > 0 || terms.extensions.length > 0;
}

function addExtension(extensions: Set<string>, value: string | undefined): void {
  const extension = normalizeExtension(value);
  if (extension) {
    extensions.add(extension);
  }
}

function looksLikeFileName(value: string): boolean {
  return !/^(?:www\.|http\b|https\b)/u.test(value);
}

function flattenScalarFields(input: unknown, prefix: readonly string[] = []): FlatField[] {
  const object = asObject(input);
  if (!object) {
    return [];
  }
  return Object.entries(object).flatMap(([key, value]) => {
    const fieldPath = [...prefix, key];
    const scalar = stringValue(value);
    if (scalar !== undefined) {
      const fieldKey = fieldPath.join(" ");
      return [
        {
          key: fieldKey,
          normalized: normalizeFieldName(fieldKey),
          tokens: fieldTokens(fieldKey),
          value: scalar
        }
      ];
    }
    if (asObject(value)) {
      return flattenScalarFields(value, fieldPath);
    }
    return [];
  });
}

function stringField(fields: readonly FlatField[], aliases: readonly string[]): string | undefined {
  const normalizedAliases = aliases.map(normalizeFieldName);
  for (const alias of normalizedAliases) {
    const exact = fields.find((field) => field.normalized === alias);
    if (exact) {
      return exact.value;
    }
  }
  for (const alias of normalizedAliases) {
    const suffix = fields.find((field) => field.normalized.endsWith(alias));
    if (suffix) {
      return suffix.value;
    }
  }
  return undefined;
}

function stringValue(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value.trim() || undefined;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  const object = asObject(value);
  if (!object) {
    return undefined;
  }
  return stringValue(
    object["#text"] ?? object._ ?? object.Value ?? object.value ?? object.Text ?? object.text
  );
}

function booleanValue(value: string): boolean | undefined {
  const normalized = value.trim().toLowerCase();
  if (["true", "yes", "y", "1", "in use", "active"].includes(normalized)) {
    return true;
  }
  if (["false", "no", "n", "0", "deleted", "inactive"].includes(normalized)) {
    return false;
  }
  return undefined;
}

function normalizePathTerm(value: string): string {
  return normalizeFileTerm(value).replace(/\//gu, "\\").replace(/\\+/gu, "\\");
}

function normalizeFileTerm(value: string): string {
  return stripTrailingPunctuation(
    value
      .trim()
      .replace(/^["']|["']$/gu, "")
      .toLowerCase()
  );
}

function normalizeExtension(value: string | undefined): string | undefined {
  const normalized = value?.trim().toLowerCase().replace(/^\./u, "");
  return normalized && /^[a-z0-9]{1,10}$/u.test(normalized) ? normalized : undefined;
}

function extensionFromName(value: string): string | undefined {
  const fileName = pathBasename(value);
  const match = fileName.match(/\.([a-z0-9]{1,10})(?::[^\\/:]+)?$/iu);
  return match?.[1]?.toLowerCase();
}

function pathBasename(value: string): string {
  return value.split(/[\\/]/u).at(-1) ?? value;
}

function combinePath(parentPath: string, fileName: string): string {
  return `${parentPath.replace(/[\\/]+$/u, "")}\\${fileName}`;
}

function stripTrailingPunctuation(value: string): string {
  return value.replace(/[),.;:]+$/u, "");
}

function normalizeFieldName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "");
}

function fieldTokens(value: string): string[] {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/gu)
    .filter(Boolean);
}

function asObject(value: unknown): JsonObject | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as JsonObject)
    : undefined;
}
