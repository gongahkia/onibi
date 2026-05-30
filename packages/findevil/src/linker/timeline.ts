import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface TimelineRow {
  readonly rowNumber: number;
  readonly fields: Readonly<Record<string, string>>;
  readonly raw: string;
}

export interface TimelineMatch {
  readonly row: TimelineRow;
  readonly supports: string;
}

export function parseTimelineCsv(input: string): TimelineRow[] {
  const records = parseCsv(input);
  const [header, ...rows] = records;
  if (!header || header.length === 0) {
    return [];
  }
  const columns = header.map((column, index) => normalizeHeader(column) || `column_${index + 1}`);
  return rows
    .filter((row) => row.some((field) => field.trim().length > 0))
    .map((row, index) => {
      const fields = Object.fromEntries(
        columns.map((column, columnIndex) => [column, row[columnIndex]?.trim() ?? ""])
      );
      return {
        rowNumber: index + 2,
        fields,
        raw: row.join(",")
      };
    });
}

export function matchClaimToRows(claimText: string, rows: readonly TimelineRow[]): TimelineMatch[] {
  const terms = claimSearchTerms(claimText);
  if (terms.length === 0) {
    return [];
  }
  return rows
    .filter((row) => terms.some((term) => rowText(row).includes(term)))
    .map((row) => ({
      row,
      supports: inferTimelineSupport(row)
    }));
}

export function timelineMatchToEvidenceRef(artifact: string, match: TimelineMatch): EvidenceRef {
  return {
    artifact,
    locator: `row:${match.row.rowNumber}`,
    supports: match.supports,
    hash: hashEvidenceRow(match.row)
  };
}

function inferTimelineSupport(row: TimelineRow): string {
  const text = rowText(row);
  if (
    text.includes("sysmon") &&
    (text.includes("process create") ||
      text.includes("process_create") ||
      text.includes("event id 1") ||
      text.includes("eventid=1") ||
      text.includes("event_id:1"))
  ) {
    return "sysmon_process_create";
  }
  if (
    text.includes("run key") ||
    text.includes("\\run\\") ||
    text.includes("currentversion\\run")
  ) {
    return "registry-run-key";
  }
  if (text.includes("scheduled task") || text.includes("taskscheduler")) {
    return "scheduled-task";
  }
  if (
    text.includes("service create") ||
    text.includes("service installed") ||
    text.includes("event id 7045")
  ) {
    return "service-create";
  }
  if (text.includes("pcap") || text.includes("netflow") || text.includes("network connection")) {
    return "netflow-or-pcap";
  }
  if (text.includes("dns query") || text.includes("dns lookup") || text.includes("query name")) {
    return "dns_lookup";
  }
  return "file_present";
}

function claimSearchTerms(text: string): string[] {
  const lower = text.toLowerCase();
  const terms = new Set<string>();
  for (const match of lower.matchAll(
    /[a-z0-9_.-]+\.(?:exe|dll|ps1|bat|cmd|vbs|js|msi|scr|sys)\b/giu
  )) {
    terms.add(match[0]);
  }
  for (const match of lower.matchAll(/[a-f0-9]{32,64}/giu)) {
    terms.add(match[0]);
  }
  for (const match of lower.matchAll(/[a-z]:\\[^\s"'`]+/giu)) {
    terms.add(match[0].replace(/[),.;:]+$/u, ""));
  }
  const hostMatches = lower.matchAll(/\b(?:[a-z0-9-]+\.)+[a-z]{2,}\b/giu);
  for (const match of hostMatches) {
    terms.add(match[0]);
  }
  return [...terms].filter((term) => term.length >= 4);
}

function rowText(row: TimelineRow): string {
  return `${row.raw}\n${Object.values(row.fields).join("\n")}`.toLowerCase();
}

function normalizeHeader(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/gu, "_")
    .replace(/^_+|_+$/gu, "");
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
