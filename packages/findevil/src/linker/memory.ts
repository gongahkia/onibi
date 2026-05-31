import { existsSync, readFileSync } from "node:fs";
import { basename } from "node:path";
import { isIP } from "node:net";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface VolatilityRecord {
  readonly plugin: string;
  readonly row: number;
  readonly sourceLocator: string;
  readonly artifact: string;
  readonly data: Record<string, string>;
  readonly raw: Readonly<Record<string, unknown>>;
}

type JsonObject = Record<string, unknown>;

const pluginNames = {
  pslist: "windows.pslist",
  netscan: "windows.netscan",
  malfind: "windows.malfind",
  cmdline: "windows.cmdline"
} as const;

export function parseVolatilityJson(file: string): VolatilityRecord[] {
  const input = volatilityInput(file);
  return parseJsonRecords(input.contents).flatMap((record) =>
    normalizeVolatilityRecords(record, input.artifact)
  );
}

export function matchVolatilityPslist(
  claim: { readonly text: string },
  records: readonly VolatilityRecord[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  return records
    .filter(
      (record) =>
        record.plugin === pluginNames.pslist &&
        processRecordMatches(claimText, record, [
          "ImageFileName",
          "Image File Name",
          "Name",
          "Process",
          "ProcessName"
        ])
    )
    .map((record) => volatilityRecordToEvidenceRef(record, "volatility-pslist"));
}

export function matchVolatilityNetscan(
  claim: Claim,
  records: readonly VolatilityRecord[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  const ips = networkIps(claim.text);
  const domains = networkDomains(claim.text);
  const ports = networkPorts(claim.text);
  return records
    .filter(
      (record) =>
        record.plugin === pluginNames.netscan &&
        netscanRecordMatches(record, claimText, ips, domains, ports)
    )
    .map((record) => volatilityRecordToEvidenceRef(record, "volatility-netscan"));
}

export function matchVolatilityMalfind(
  claim: { readonly text: string },
  records: readonly VolatilityRecord[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  return records
    .filter(
      (record) =>
        record.plugin === pluginNames.malfind &&
        processRecordMatches(claimText, record, ["Process", "Name", "ImageFileName"])
    )
    .map((record) => volatilityRecordToEvidenceRef(record, "volatility-malfind"));
}

export function matchVolatilityCmdline(
  claim: { readonly text: string },
  records: readonly VolatilityRecord[]
): EvidenceRef[] {
  const claimText = claim.text.toLowerCase();
  return records
    .filter(
      (record) =>
        record.plugin === pluginNames.cmdline &&
        (processRecordMatches(claimText, record, [
          "Process",
          "Name",
          "ImageFileName",
          "CommandLine",
          "CmdLine",
          "Cmd",
          "Args"
        ]) ||
          commandLineMatches(claimText, record))
    )
    .map((record) => volatilityRecordToEvidenceRef(record, "volatility-cmdline"));
}

function volatilityInput(file: string): { contents: string; artifact: string } {
  const trimmed = file.trimStart();
  if (
    !trimmed.startsWith("{") &&
    !trimmed.startsWith("[") &&
    !file.includes("\n") &&
    existsSync(file)
  ) {
    return { contents: readFileSync(file, "utf8"), artifact: file };
  }
  return { contents: file, artifact: "volatility.json" };
}

function parseJsonRecords(input: string): JsonObject[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return collectJsonObjects(JSON.parse(trimmed));
  } catch {
    return trimmed.split(/\r?\n/u).flatMap((line) => {
      const candidate = line.trim();
      if (candidate.length === 0) {
        return [];
      }
      try {
        return collectJsonObjects(JSON.parse(candidate));
      } catch {
        return [];
      }
    });
  }
}

function collectJsonObjects(value: unknown): JsonObject[] {
  if (Array.isArray(value)) {
    return value.filter(isObject);
  }
  return isObject(value) ? [value] : [];
}

function normalizeVolatilityRecords(record: JsonObject, artifact: string): VolatilityRecord[] {
  const wrapperPlugin = canonicalPlugin(pluginFromRecord(record) ?? pluginFromArtifact(artifact));
  const columns = columnNames(record);
  const rawRows = rowValues(record);
  const rowObjects = rawRows.flatMap((row) => flattenRow(row, columns));
  const rows = rowObjects.length > 0 ? rowObjects : [stripTreeChildren(record)];
  return rows.flatMap((row, index): VolatilityRecord[] => {
    const plugin = canonicalPlugin(pluginFromRecord(row) ?? wrapperPlugin);
    if (!plugin) {
      return [];
    }
    const rowNumber = index + 1;
    return [
      {
        plugin,
        row: rowNumber,
        sourceLocator: `volatility:plugin=${plugin}:row=${rowNumber}`,
        artifact,
        data: scalarData(row),
        raw: row
      }
    ];
  });
}

function columnNames(record: JsonObject): string[] {
  const columns = firstArray(record, ["columns", "Columns"]);
  if (!columns) {
    return [];
  }
  return columns.flatMap((column, index): string[] => {
    if (typeof column === "string") {
      return [column];
    }
    if (!isObject(column)) {
      return [`col${index}`];
    }
    const name = stringField(column, ["name", "Name", "key", "Key", "title", "Title"]);
    return [name ?? `col${index}`];
  });
}

function rowValues(record: JsonObject): unknown[] {
  for (const key of ["rows", "Rows", "records", "Records", "items", "Items", "data", "Data"]) {
    const rows = record[key];
    if (Array.isArray(rows)) {
      return rows;
    }
  }
  return [];
}

function flattenRow(row: unknown, columns: readonly string[]): JsonObject[] {
  const normalized = normalizeRow(row, columns);
  if (!normalized) {
    return [];
  }
  const children = firstArray(normalized, ["__children", "children", "Children"]);
  return [
    stripTreeChildren(normalized),
    ...(children?.flatMap((child) => flattenRow(child, columns)) ?? [])
  ];
}

function normalizeRow(row: unknown, columns: readonly string[]): JsonObject | undefined {
  if (Array.isArray(row)) {
    return Object.fromEntries(row.map((value, index) => [columns[index] ?? `col${index}`, value]));
  }
  if (isObject(row)) {
    return row;
  }
  return undefined;
}

function stripTreeChildren(record: JsonObject): JsonObject {
  return Object.fromEntries(
    Object.entries(record).filter(
      ([key]) =>
        !["__children", "children", "Children", "rows", "Rows", "columns", "Columns"].includes(key)
    )
  );
}

function scalarData(record: JsonObject): Record<string, string> {
  const data: Record<string, string> = {};
  for (const [key, value] of Object.entries(record)) {
    if (["__children", "children", "Children"].includes(key)) {
      continue;
    }
    const scalar = stringValue(value);
    if (scalar !== undefined) {
      data[key] = scalar;
    }
  }
  return data;
}

function processRecordMatches(
  claimText: string,
  record: VolatilityRecord,
  fields: readonly string[]
): boolean {
  const terms = processTerms(record, fields);
  if (terms.some((term) => includesTerm(claimText, term))) {
    return true;
  }
  const pid = fieldValue(record, ["PID", "Pid", "ProcessId", "Process ID"]);
  return pid ? includesPid(claimText, pid) : false;
}

function processTerms(record: VolatilityRecord, fields: readonly string[]): string[] {
  const values = fields.flatMap((field) => fieldValues(record, [field]));
  return uniqueTerms(values.flatMap((value) => [value, pathBasename(value)]));
}

function commandLineMatches(claimText: string, record: VolatilityRecord): boolean {
  return fieldValues(record, ["CommandLine", "CmdLine", "Cmd", "Args"]).some((commandLine) => {
    const exeTerms = commandLine.match(/[a-z0-9_.-]+\.exe\b/giu) ?? [];
    const switchTerms = commandLine.match(/(?:^|\s)(?:-{1,2}|\/)[a-z0-9][a-z0-9-]*/giu) ?? [];
    return [...exeTerms, ...switchTerms].some((term) => includesTerm(claimText, term.trim()));
  });
}

function netscanRecordMatches(
  record: VolatilityRecord,
  claimText: string,
  ips: ReadonlySet<string>,
  domains: ReadonlySet<string>,
  ports: ReadonlySet<string>
): boolean {
  const addressValues = fieldValues(record, [
    "LocalAddr",
    "Local Address",
    "ForeignAddr",
    "Foreign Address",
    "RemoteAddr",
    "Remote Address",
    "ForeignHost",
    "RemoteHost"
  ]).map((value) => value.toLowerCase());
  if (addressValues.some((value) => ips.has(value) || domains.has(value))) {
    return true;
  }
  if (
    fieldValues(record, [
      "LocalPort",
      "Local Port",
      "ForeignPort",
      "Foreign Port",
      "RemotePort"
    ]).some((port) => ports.has(port.trim()))
  ) {
    return true;
  }
  return processRecordMatches(claimText, record, ["Owner", "Process", "Name", "ImageFileName"]);
}

function volatilityRecordToEvidenceRef(record: VolatilityRecord, supports: string): EvidenceRef {
  return {
    artifact: record.artifact,
    locator: record.sourceLocator,
    supports,
    hash: hashEvidenceRow(record)
  };
}

function fieldValues(record: VolatilityRecord, fields: readonly string[]): string[] {
  return fields.flatMap((field) => {
    const value = fieldValue(record, [field]);
    return value ? [value] : [];
  });
}

function fieldValue(record: VolatilityRecord, fields: readonly string[]): string | undefined {
  for (const field of fields) {
    const value = caseInsensitiveValue(record.data, field);
    if (value) {
      return value;
    }
  }
  return undefined;
}

function caseInsensitiveValue(record: Record<string, string>, field: string): string | undefined {
  const direct = record[field];
  if (direct !== undefined) {
    return direct;
  }
  const lower = normalizeKey(field);
  const match = Object.entries(record).find(([key]) => normalizeKey(key) === lower);
  return match?.[1];
}

function firstArray(source: JsonObject, keys: readonly string[]): unknown[] | undefined {
  for (const key of keys) {
    const value = source[key];
    if (Array.isArray(value)) {
      return value;
    }
  }
  return undefined;
}

function pluginFromRecord(record: JsonObject): string | undefined {
  const direct = stringField(record, [
    "plugin",
    "Plugin",
    "pluginName",
    "plugin_name",
    "volatilityPlugin",
    "volatility_plugin"
  ]);
  if (direct) {
    return direct;
  }
  const metadata = firstObject(record, ["metadata", "Metadata", "meta", "Meta"]);
  return metadata
    ? stringField(metadata, ["plugin", "Plugin", "pluginName", "plugin_name"])
    : undefined;
}

function pluginFromArtifact(artifact: string): string | undefined {
  const lower = basename(artifact).toLowerCase();
  if (lower.includes("pslist")) {
    return pluginNames.pslist;
  }
  if (lower.includes("netscan")) {
    return pluginNames.netscan;
  }
  if (lower.includes("malfind")) {
    return pluginNames.malfind;
  }
  if (lower.includes("cmdline")) {
    return pluginNames.cmdline;
  }
  return undefined;
}

function canonicalPlugin(plugin: string | undefined): string | undefined {
  if (!plugin) {
    return undefined;
  }
  const lower = plugin.trim().toLowerCase();
  for (const known of Object.values(pluginNames)) {
    if (lower.includes(known)) {
      return known;
    }
  }
  return lower;
}

function networkIps(text: string): Set<string> {
  const terms = new Set<string>();
  for (const match of text.matchAll(/\b(?:\d{1,3}\.){3}\d{1,3}\b/gu)) {
    if (isIP(match[0]) === 4) {
      terms.add(match[0].toLowerCase());
    }
  }
  for (const match of text.matchAll(/\b[0-9a-f:]{2,}:[0-9a-f:.]+\b/giu)) {
    if (isIP(match[0]) === 6) {
      terms.add(match[0].toLowerCase());
    }
  }
  return terms;
}

function networkDomains(text: string): Set<string> {
  const domains = new Set<string>();
  for (const match of text.matchAll(/\b(?:[a-z0-9-]+\.)+[a-z]{2,}\b/giu)) {
    if (!looksLikeExecutable(match[0])) {
      domains.add(match[0].toLowerCase());
    }
  }
  return domains;
}

function networkPorts(text: string): Set<string> {
  const ports = new Set<string>();
  for (const match of text.matchAll(/(?:^|[^0-9])(\d{1,5})(?=$|[^0-9])/gu)) {
    const port = Number(match[1]);
    if (port > 0 && port <= 65535) {
      ports.add(String(port));
    }
  }
  return ports;
}

function looksLikeExecutable(value: string): boolean {
  return /\.(?:bat|cmd|dll|exe|js|msi|ps1|scr|sys|vbs)$/iu.test(value);
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

function includesPid(text: string, pid: string): boolean {
  const normalized = pid.trim();
  if (!/^\d+$/u.test(normalized)) {
    return false;
  }
  return new RegExp(
    `\\b(?:pid|process(?: id)?)\\s*[:#=]?\\s*${escapeRegExp(normalized)}\\b`,
    "u"
  ).test(text);
}

function uniqueTerms(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim().toLowerCase()).filter(Boolean))];
}

function pathBasename(path: string): string {
  return path.split(/[\\/]/u).at(-1) ?? path;
}

function firstObject(source: JsonObject, keys: readonly string[]): JsonObject | undefined {
  for (const key of keys) {
    const value = source[key];
    if (isObject(value)) {
      return value;
    }
  }
  return undefined;
}

function stringField(source: JsonObject, keys: readonly string[]): string | undefined {
  for (const key of keys) {
    const value = stringValue(source[key]);
    if (value !== undefined) {
      return value;
    }
  }
  return undefined;
}

function stringValue(value: unknown): string | undefined {
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : undefined;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (isObject(value)) {
    return stringField(value, ["#text", "_", "Value", "value", "Text", "text"]);
  }
  return undefined;
}

function normalizeKey(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]/gu, "");
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
