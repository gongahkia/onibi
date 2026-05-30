import type { EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface SrumNetworkEntry {
  readonly appId: string;
  readonly application?: string | undefined;
  readonly hour: string;
  readonly timestamp?: string | undefined;
  readonly bytesSent?: number | undefined;
  readonly bytesReceived?: number | undefined;
  readonly sourceLocator: string;
  readonly raw: string;
}

interface DraftSrumNetworkEntry {
  appId?: string | undefined;
  application?: string | undefined;
  timestamp?: string | undefined;
  hour?: string | undefined;
  bytesSent?: number | undefined;
  bytesReceived?: number | undefined;
  rawLines?: string[] | undefined;
}

export function parseSrumOutput(input: string): SrumNetworkEntry[] {
  const csvEntries = parseCsvLikeSrum(input);
  if (csvEntries.length > 0) {
    return csvEntries;
  }
  return parseTextSrum(input);
}

export function matchBySrumApp(
  claimText: string,
  entries: readonly SrumNetworkEntry[]
): SrumNetworkEntry[] {
  const terms = claimTerms(claimText);
  if (terms.length === 0) {
    return [];
  }
  return entries.filter((entry) => {
    const haystack = [entry.appId, entry.application].filter(Boolean).join("\n").toLowerCase();
    return terms.some((term) => haystack.includes(term));
  });
}

export function srumEntryToEvidenceRef(artifact: string, entry: SrumNetworkEntry): EvidenceRef {
  return {
    artifact,
    locator: entry.sourceLocator,
    supports: "srum_network_activity",
    hash: hashEvidenceRow(entry)
  };
}

function parseTextSrum(input: string): SrumNetworkEntry[] {
  const entries: SrumNetworkEntry[] = [];
  const lines = input.split(/\r?\n/u);
  let current: DraftSrumNetworkEntry = {};
  const flush = (): void => {
    const appId = current.appId ?? current.application;
    const hour = current.hour ?? timestampToHour(current.timestamp);
    if (!appId || !hour || !hasNetworkActivity(current)) {
      current = {};
      return;
    }
    entries.push({
      appId: appId.trim(),
      ...(current.application ? { application: current.application.trim() } : {}),
      hour,
      ...(current.timestamp ? { timestamp: current.timestamp } : {}),
      ...(current.bytesSent !== undefined ? { bytesSent: current.bytesSent } : {}),
      ...(current.bytesReceived !== undefined ? { bytesReceived: current.bytesReceived } : {}),
      sourceLocator: srumLocator(appId.trim(), hour),
      raw: current.rawLines?.join("\n") ?? appId
    });
    current = {};
  };

  lines.forEach((line) => {
    const trimmed = line.trim();
    if (trimmed.length === 0) {
      flush();
      return;
    }
    current.rawLines = [...(current.rawLines ?? []), line];
    const appId = fieldValue(trimmed, /^(?:app\s*id|appid)\s*[:=]\s*(.+)$/iu);
    if (appId) {
      current.appId = appId;
    }
    const application = fieldValue(
      trimmed,
      /^(?:application(?: name)?|image(?: path)?|process(?: name)?|path|file path)\s*[:=]\s*(.+)$/iu
    );
    if (application) {
      current.application = application;
    }
    const timestamp = fieldValue(
      trimmed,
      /^(?:timestamp|time stamp|date(?: time)?|datetime|time|start time|end time)\s*[:=]\s*(.+)$/iu
    );
    if (timestamp) {
      current.timestamp = timestamp;
    }
    const hour = fieldValue(trimmed, /^hour(?: utc)?\s*[:=]\s*(.+)$/iu);
    if (hour) {
      current.hour = timestampToHour(hour);
    }
    const bytesSent = fieldValue(trimmed, /^(?:bytes sent|sent bytes|bytessent)\s*[:=]\s*(.+)$/iu);
    if (bytesSent) {
      current.bytesSent = parseByteCount(bytesSent);
    }
    const bytesReceived = fieldValue(
      trimmed,
      /^(?:bytes received|received bytes|bytesreceived|bytes recv|bytesrecv)\s*[:=]\s*(.+)$/iu
    );
    if (bytesReceived) {
      current.bytesReceived = parseByteCount(bytesReceived);
    }
  });
  flush();
  return entries;
}

function parseCsvLikeSrum(input: string): SrumNetworkEntry[] {
  const [header, ...dataRows] = parseCsv(input.trim());
  if (!header || header.length < 2) {
    return [];
  }
  const headers = header.map(normalizeHeader);
  const appIdIndex = firstIndex(headers, ["appid", "app_id"]);
  const applicationIndex = firstIndex(headers, [
    "application",
    "application_name",
    "image",
    "image_path",
    "process",
    "process_name",
    "path",
    "file_path",
    "name"
  ]);
  const timestampIndex = firstIndex(headers, [
    "timestamp",
    "time_stamp",
    "date_time",
    "datetime",
    "time",
    "start_time",
    "end_time",
    "hour",
    "hour_utc"
  ]);
  if ((appIdIndex < 0 && applicationIndex < 0) || timestampIndex < 0) {
    return [];
  }
  const bytesSentIndex = firstIndex(headers, ["bytes_sent", "sent_bytes", "bytessent"]);
  const bytesReceivedIndex = firstIndex(headers, [
    "bytes_received",
    "received_bytes",
    "bytesreceived",
    "bytes_recv",
    "bytesrecv"
  ]);
  return dataRows.flatMap((row): SrumNetworkEntry[] => {
    const appId = fieldAt(row, appIdIndex) ?? fieldAt(row, applicationIndex);
    const application = fieldAt(row, applicationIndex);
    const timestamp = fieldAt(row, timestampIndex);
    const hour = timestampToHour(timestamp);
    const bytesSent = parseByteCount(fieldAt(row, bytesSentIndex));
    const bytesReceived = parseByteCount(fieldAt(row, bytesReceivedIndex));
    if (
      !appId ||
      !hour ||
      !hasNetworkActivity({
        bytesSent,
        bytesReceived
      })
    ) {
      return [];
    }
    return [
      {
        appId,
        ...(application && application !== appId ? { application } : {}),
        hour,
        ...(timestamp ? { timestamp } : {}),
        ...(bytesSent !== undefined ? { bytesSent } : {}),
        ...(bytesReceived !== undefined ? { bytesReceived } : {}),
        sourceLocator: srumLocator(appId, hour),
        raw: row.join(",")
      }
    ];
  });
}

function hasNetworkActivity(
  entry: Pick<DraftSrumNetworkEntry, "bytesSent" | "bytesReceived">
): boolean {
  if (entry.bytesSent === undefined && entry.bytesReceived === undefined) {
    return true;
  }
  return (entry.bytesSent ?? 0) > 0 || (entry.bytesReceived ?? 0) > 0;
}

function srumLocator(appId: string, hour: string): string {
  return `srum:appid=${appId}:hour=${hour}`;
}

function timestampToHour(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  const trimmed = value.trim();
  const normalized = /^\d{4}-\d{2}-\d{2}$/u.test(trimmed)
    ? `${trimmed}T00:00:00Z`
    : trimmed.replace(" ", "T");
  const zoned = /(?:z|[+-]\d{2}:?\d{2})$/iu.test(normalized) ? normalized : `${normalized}Z`;
  const date = new Date(zoned);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  date.setUTCMinutes(0, 0, 0);
  return date.toISOString();
}

function parseByteCount(value: string | undefined): number | undefined {
  if (!value) {
    return undefined;
  }
  const parsed = Number(value.replace(/,/gu, "").trim());
  return Number.isFinite(parsed) ? parsed : undefined;
}

function fieldAt(row: readonly string[], index: number): string | undefined {
  if (index < 0) {
    return undefined;
  }
  return row[index]?.trim() || undefined;
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
