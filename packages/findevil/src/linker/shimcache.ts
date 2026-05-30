import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface ShimcacheEntry {
  readonly path: string;
  readonly lastModified?: string | undefined;
  readonly sourceLocator: string;
  readonly raw: string;
}

interface DraftShimcacheEntry {
  path?: string | undefined;
  lastModified?: string | undefined;
  sourceLocator?: string | undefined;
  rawLines?: string[] | undefined;
}

export function parseShimcacheOutput(input: string): ShimcacheEntry[] {
  const csvEntries = parseCsvLikeShimcache(input);
  if (csvEntries.length > 0) {
    return csvEntries;
  }
  return parseTextShimcache(input);
}

export function matchByShimcachePath(
  claimText: string,
  entries: readonly ShimcacheEntry[]
): ShimcacheEntry[] {
  const terms = claimTerms(claimText);
  if (terms.length === 0) {
    return [];
  }
  return entries.filter((entry) => terms.some((term) => entry.path.includes(term)));
}

export function shimcacheEntryToEvidenceRef(artifact: string, entry: ShimcacheEntry): EvidenceRef {
  return {
    artifact,
    locator: entry.sourceLocator,
    supports: "shimcache_indicator",
    hash: hashEvidenceRow(entry)
  };
}

function parseTextShimcache(input: string): ShimcacheEntry[] {
  const entries: ShimcacheEntry[] = [];
  const lines = input.split(/\r?\n/u);
  let current: DraftShimcacheEntry = {};
  const flush = (): void => {
    if (!current.path) {
      current = {};
      return;
    }
    entries.push({
      path: current.path,
      ...(current.lastModified ? { lastModified: current.lastModified } : {}),
      sourceLocator: current.sourceLocator ?? "shimcache:row=1",
      raw: current.rawLines?.join("\n") ?? current.path
    });
    current = {};
  };

  lines.forEach((line, index) => {
    const trimmed = line.trim();
    if (trimmed.length === 0) {
      flush();
      return;
    }
    current.rawLines = [...(current.rawLines ?? []), line];
    current.sourceLocator ??= `shimcache:row=${index + 1}`;
    const path =
      fieldValue(
        trimmed,
        /^(?:path|file path|full path|name|application(?: path)?)\s*[:=]\s*(.+)$/iu
      ) ?? executablePathInText(trimmed);
    if (path) {
      current.path = normalizePath(path);
    }
    const lastModified = fieldValue(
      trimmed,
      /^(?:last modified|modified(?: time)?|mtime|lastmod)\s*[:=]\s*(.+)$/iu
    );
    if (lastModified) {
      current.lastModified = lastModified;
    }
  });
  flush();
  return entries;
}

function parseCsvLikeShimcache(input: string): ShimcacheEntry[] {
  const [header, ...dataRows] = parseCsv(input.trim());
  if (!header || header.length < 2) {
    return [];
  }
  const headers = header.map(normalizeHeader);
  const pathIndex = firstIndex(headers, [
    "path",
    "file_path",
    "full_path",
    "name",
    "application_path"
  ]);
  if (pathIndex < 0) {
    return [];
  }
  const lastModifiedIndex = firstIndex(headers, [
    "last_modified",
    "modified_time",
    "mtime",
    "lastmod"
  ]);
  return dataRows.flatMap((row, index): ShimcacheEntry[] => {
    const path = row[pathIndex]?.trim();
    if (!path) {
      return [];
    }
    const lastModified = lastModifiedIndex >= 0 ? row[lastModifiedIndex]?.trim() : undefined;
    return [
      {
        path: normalizePath(path),
        ...(lastModified ? { lastModified } : {}),
        sourceLocator: `shimcache:row=${index + 2}`,
        raw: row.join(",")
      }
    ];
  });
}

function claimTerms(text: string): string[] {
  const lower = text.toLowerCase();
  const terms = new Set<string>();
  for (const match of lower.matchAll(/[a-z]:\\[^\s"'`]+/giu)) {
    terms.add(match[0].replace(/[),.;:]+$/u, ""));
  }
  for (const match of lower.matchAll(
    /[a-z0-9_.-]+\.(?:exe|dll|ps1|bat|cmd|vbs|js|msi|scr|sys)\b/giu
  )) {
    terms.add(match[0]);
  }
  return [...terms].filter((term) => term.length >= 4);
}

function executablePathInText(value: string): string | undefined {
  return value.match(/[a-z]:\\[^\r\n"'`]+?\.(?:exe|dll|scr|com|sys)\b/iu)?.[0];
}

function normalizePath(value: string): string {
  return value
    .trim()
    .replace(/^["']|["']$/gu, "")
    .toLowerCase();
}

function fieldValue(line: string, pattern: RegExp): string | undefined {
  return line.match(pattern)?.[1]?.trim();
}

function normalizeHeader(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/gu, "_")
    .replace(/^_+|_+$/gu, "");
}

function firstIndex(values: readonly string[], candidates: readonly string[]): number {
  return values.findIndex((value) => candidates.includes(value));
}

function parseCsv(input: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let inQuotes = false;
  for (let index = 0; index < input.length; index += 1) {
    const char = input[index];
    const next = input[index + 1];
    if (char === '"' && inQuotes && next === '"') {
      field += '"';
      index += 1;
      continue;
    }
    if (char === '"') {
      inQuotes = !inQuotes;
      continue;
    }
    if (char === "," && !inQuotes) {
      row.push(field);
      field = "";
      continue;
    }
    if ((char === "\n" || char === "\r") && !inQuotes) {
      if (char === "\r" && next === "\n") {
        index += 1;
      }
      row.push(field);
      rows.push(row);
      row = [];
      field = "";
      continue;
    }
    field += char;
  }
  if (field.length > 0 || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}
