import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface AmcacheEntry {
  readonly path?: string | undefined;
  readonly sha1?: string | undefined;
  readonly sha256?: string | undefined;
  readonly sourceLocator: string;
  readonly raw: string;
}

interface DraftAmcacheEntry {
  path?: string | undefined;
  sha1?: string | undefined;
  sha256?: string | undefined;
  sourceLocator?: string | undefined;
  rawLines?: string[] | undefined;
}

export function parseAmcacheOutput(input: string): AmcacheEntry[] {
  const csvEntries = parseCsvLikeAmcache(input);
  if (csvEntries.length > 0) {
    return csvEntries;
  }
  return parseTextAmcache(input);
}

export function matchByPathOrHash(
  claimText: string,
  entries: readonly AmcacheEntry[]
): AmcacheEntry[] {
  const terms = claimTerms(claimText);
  if (terms.length === 0) {
    return [];
  }
  return entries.filter((entry) => {
    const haystack = [entry.path, entry.sha1, entry.sha256]
      .filter(Boolean)
      .join("\n")
      .toLowerCase();
    return terms.some((term) => haystack.includes(term));
  });
}

export function amcacheEntryToEvidenceRef(artifact: string, entry: AmcacheEntry): EvidenceRef {
  return {
    artifact,
    locator: entry.sourceLocator,
    supports: "amcache_execution_record",
    hash: hashEvidenceRow(entry)
  };
}

function parseTextAmcache(input: string): AmcacheEntry[] {
  const entries: AmcacheEntry[] = [];
  const lines = input.split(/\r?\n/u);
  let current: DraftAmcacheEntry = {};
  const flush = (): void => {
    if (!current.path && !current.sha1 && !current.sha256) {
      current = {};
      return;
    }
    entries.push({
      ...(current.path ? { path: current.path } : {}),
      ...(current.sha1 ? { sha1: current.sha1 } : {}),
      ...(current.sha256 ? { sha256: current.sha256 } : {}),
      sourceLocator: current.sourceLocator ?? "line:1",
      raw: current.rawLines?.join("\n") ?? current.path ?? current.sha1 ?? current.sha256 ?? ""
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
    current.sourceLocator ??= `line:${index + 1}`;
    const path = fieldValue(
      trimmed,
      /^(?:path|full path|lowercase long path|file path)\s*[:=]\s*(.+)$/iu
    );
    if (path) {
      current.path = path.toLowerCase();
    }
    const sha1 = fieldValue(trimmed, /^sha1\s*[:=]\s*([a-f0-9]{40})$/iu);
    if (sha1) {
      current.sha1 = sha1.toLowerCase();
    }
    const sha256 = fieldValue(trimmed, /^sha256\s*[:=]\s*([a-f0-9]{64})$/iu);
    if (sha256) {
      current.sha256 = sha256.toLowerCase();
    }
  });
  flush();
  return entries;
}

function parseCsvLikeAmcache(input: string): AmcacheEntry[] {
  const [headerLine, ...dataLines] = input.trim().split(/\r?\n/u);
  if (!headerLine?.includes(",")) {
    return [];
  }
  const headers = headerLine.split(",").map(normalizeHeader);
  const pathIndex = firstIndex(headers, ["path", "full_path", "lowercase_long_path", "file_path"]);
  const sha1Index = firstIndex(headers, ["sha1", "sha_1"]);
  const sha256Index = firstIndex(headers, ["sha256", "sha_256"]);
  if (pathIndex < 0 && sha1Index < 0 && sha256Index < 0) {
    return [];
  }
  const entries: AmcacheEntry[] = [];
  dataLines.forEach((line, index) => {
    const fields = line.split(",").map((field) => field.trim());
    const path = pathIndex >= 0 ? fields[pathIndex]?.toLowerCase() : undefined;
    const sha1 = sha1Index >= 0 ? fields[sha1Index]?.toLowerCase() : undefined;
    const sha256 = sha256Index >= 0 ? fields[sha256Index]?.toLowerCase() : undefined;
    if (!path && !sha1 && !sha256) {
      return;
    }
    entries.push({
      ...(path ? { path } : {}),
      ...(sha1 ? { sha1 } : {}),
      ...(sha256 ? { sha256 } : {}),
      sourceLocator: `row:${index + 2}`,
      raw: line
    });
  });
  return entries;
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
  for (const match of lower.matchAll(/[a-f0-9]{40,64}/giu)) {
    terms.add(match[0]);
  }
  return [...terms].filter((term) => term.length >= 4);
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
