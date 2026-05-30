import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface PrefetchEntry {
  readonly executable: string;
  readonly runCount?: number | undefined;
  readonly lastRunTimes: readonly string[];
  readonly sourceLocator: string;
  readonly raw: string;
}

interface DraftPrefetchEntry {
  executable?: string | undefined;
  runCount?: number | undefined;
  lastRunTimes?: string[] | undefined;
  sourceLocator?: string | undefined;
  rawLines?: string[] | undefined;
}

export function parsePrefetchOutput(input: string): PrefetchEntry[] {
  const csvEntries = parseCsvLikePrefetch(input);
  if (csvEntries.length > 0) {
    return csvEntries;
  }
  return parseTextPrefetch(input);
}

export function matchByExecutable(
  claimText: string,
  entries: readonly PrefetchEntry[]
): PrefetchEntry[] {
  const executables = executableTerms(claimText);
  if (executables.length === 0) {
    return [];
  }
  return entries.filter((entry) =>
    executables.some((executable) => entry.executable.toLowerCase() === executable)
  );
}

export function prefetchEntryToEvidenceRef(artifact: string, entry: PrefetchEntry): EvidenceRef {
  return {
    artifact,
    locator: entry.sourceLocator,
    supports: "prefetch_entry",
    hash: hashEvidenceRow(entry)
  };
}

function parseTextPrefetch(input: string): PrefetchEntry[] {
  const entries: PrefetchEntry[] = [];
  const lines = input.split(/\r?\n/u);
  let current: DraftPrefetchEntry = {};
  const flush = (): void => {
    if (!current.executable) {
      current = {};
      return;
    }
    entries.push({
      executable: current.executable,
      ...(current.runCount !== undefined ? { runCount: current.runCount } : {}),
      lastRunTimes: current.lastRunTimes ?? [],
      sourceLocator: current.sourceLocator ?? "line:1",
      raw: current.rawLines?.join("\n") ?? current.executable
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
    const executable =
      fieldValue(
        trimmed,
        /^(?:executable(?: name)?|application|process(?: name)?)\s*[:=]\s*(.+)$/iu
      ) ?? prefetchFilenameExecutable(trimmed);
    if (executable) {
      current.executable = executable.toLowerCase();
    }
    const runCount = fieldValue(trimmed, /^run count\s*[:=]\s*(\d+)/iu);
    if (runCount) {
      current.runCount = Number(runCount);
    }
    const lastRun = fieldValue(trimmed, /^(?:last run(?: time)?|last execution)\s*[:=]\s*(.+)$/iu);
    if (lastRun) {
      current.lastRunTimes = [...(current.lastRunTimes ?? []), lastRun];
    }
  });
  flush();
  return entries;
}

function parseCsvLikePrefetch(input: string): PrefetchEntry[] {
  const [headerLine, ...dataLines] = input.trim().split(/\r?\n/u);
  if (!headerLine?.includes(",")) {
    return [];
  }
  const headers = headerLine.split(",").map(normalizeHeader);
  const executableIndex = firstIndex(headers, [
    "executable",
    "executable_name",
    "process",
    "process_name"
  ]);
  if (executableIndex < 0) {
    return [];
  }
  const runCountIndex = firstIndex(headers, ["run_count", "runcount"]);
  const lastRunIndex = firstIndex(headers, ["last_run", "last_run_time", "last_execution"]);
  const entries: PrefetchEntry[] = [];
  dataLines.forEach((line, index) => {
    const fields = line.split(",").map((field) => field.trim());
    const executable = fields[executableIndex]?.toLowerCase();
    if (!executable) {
      return;
    }
    const runCount = runCountIndex >= 0 ? Number(fields[runCountIndex]) : undefined;
    const lastRun = lastRunIndex >= 0 ? fields[lastRunIndex] : undefined;
    entries.push({
      executable,
      ...(Number.isFinite(runCount) ? { runCount } : {}),
      lastRunTimes: lastRun ? [lastRun] : [],
      sourceLocator: `row:${index + 2}`,
      raw: line
    });
  });
  return entries;
}

function prefetchFilenameExecutable(value: string): string | undefined {
  const match = value.match(/\b([a-z0-9_.-]+\.(?:exe|dll|scr|com))(?:-[a-f0-9]+)?\.pf\b/iu);
  return match?.[1]?.toLowerCase();
}

function executableTerms(text: string): string[] {
  return [
    ...text.toLowerCase().matchAll(/[a-z0-9_.-]+\.(?:exe|dll|ps1|bat|cmd|vbs|js|msi|scr|sys)\b/giu)
  ].map((match) => match[0]);
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
