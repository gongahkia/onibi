import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { isIP } from "node:net";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface FlowSummary {
  readonly sourceFile: string;
  readonly sourceLocator: string;
  readonly srcIp: string;
  readonly srcPort?: number | undefined;
  readonly destIp?: string | undefined;
  readonly destPort?: number | undefined;
  readonly destDomain?: string | undefined;
  readonly protocol: string;
  readonly process?: string | undefined;
  readonly user?: string | undefined;
  readonly startTime?: string | undefined;
  readonly endTime?: string | undefined;
  readonly bytesOut?: number | undefined;
  readonly bytesIn?: number | undefined;
  readonly raw: Readonly<Record<string, unknown>>;
}

export function parseFlowSummaryJson(file: string): FlowSummary[] {
  const parsed: unknown = JSON.parse(readFileSync(file, "utf8"));
  return flowRecords(parsed).flatMap((record, index) => {
    const flow = normalizeFlowRecord(file, record, index);
    return flow ? [flow] : [];
  });
}

export function matchPcapNetworkConnection(
  claim: Claim,
  flows: readonly FlowSummary[]
): EvidenceRef[] {
  const destIps = destinationIps(claim.text);
  const destDomains = destinationDomains(claim.text);
  if (destIps.size === 0 && destDomains.size === 0) {
    return [];
  }
  return flows
    .filter(
      (flow) =>
        (flow.destIp ? destIps.has(flow.destIp.toLowerCase()) : false) ||
        (flow.destDomain ? destDomains.has(flow.destDomain.toLowerCase()) : false)
    )
    .map((flow) => ({
      artifact: flow.sourceFile,
      locator: `pcap:flow=${fiveTupleHash(flow)}`,
      supports: "netflow-or-pcap",
      hash: hashEvidenceRow(evidenceHashInput(flow))
    }));
}

function flowRecords(parsed: unknown): ReadonlyArray<Record<string, unknown>> {
  if (Array.isArray(parsed)) {
    return parsed.filter(isRecord);
  }
  if (!isRecord(parsed)) {
    return [];
  }
  const flows = parsed.flows ?? parsed.records ?? parsed.connections;
  if (Array.isArray(flows)) {
    return flows.filter(isRecord);
  }
  return [];
}

function normalizeFlowRecord(
  file: string,
  record: Record<string, unknown>,
  index: number
): FlowSummary | undefined {
  const srcIp = firstString(record, [
    "srcIp",
    "sourceIp",
    "source_ip",
    "src_ip",
    "src",
    "ip.src",
    "id.orig_h",
    "orig_h"
  ]);
  const destIp = firstString(record, [
    "destIp",
    "dstIp",
    "destinationIp",
    "destination_ip",
    "dest_ip",
    "dst_ip",
    "ip.dst",
    "id.resp_h",
    "resp_h"
  ]);
  const destDomain = normalizeDomain(
    firstString(record, [
      "destHost",
      "destDomain",
      "destinationHost",
      "destinationDomain",
      "host",
      "hostname",
      "domain",
      "query",
      "dns.qry.name",
      "http.host",
      "tls.server_name",
      "server_name",
      "sni"
    ])
  );
  if (!srcIp || (!destIp && !destDomain)) {
    return undefined;
  }

  const srcPort = firstNumber(record, [
    "srcPort",
    "sourcePort",
    "source_port",
    "src_port",
    "tcp.srcport",
    "udp.srcport",
    "id.orig_p",
    "orig_p"
  ]);
  const destPort = firstNumber(record, [
    "destPort",
    "dstPort",
    "destinationPort",
    "destination_port",
    "dest_port",
    "dst_port",
    "tcp.dstport",
    "udp.dstport",
    "id.resp_p",
    "resp_p"
  ]);
  const protocol =
    normalizeProtocol(
      firstString(record, ["protocol", "proto", "transport", "_ws.col.Protocol"])
    ) ?? inferProtocol(record);
  const process = firstString(record, [
    "process",
    "processName",
    "process_name",
    "app",
    "application"
  ]);
  const user = firstString(record, ["user", "username", "account"]);
  const startTime = firstString(record, ["startTime", "start_time", "ts", "timestamp"]);
  const endTime = firstString(record, ["endTime", "end_time"]);
  const bytesOut = firstNumber(record, ["bytesOut", "bytes_out", "orig_bytes"]);
  const bytesIn = firstNumber(record, ["bytesIn", "bytes_in", "resp_bytes"]);

  return {
    sourceFile: file,
    sourceLocator: sourceLocator(record, index),
    srcIp: srcIp.toLowerCase(),
    ...(srcPort !== undefined ? { srcPort } : {}),
    ...(destIp ? { destIp: destIp.toLowerCase() } : {}),
    ...(destPort !== undefined ? { destPort } : {}),
    ...(destDomain ? { destDomain } : {}),
    protocol,
    ...(process ? { process } : {}),
    ...(user ? { user } : {}),
    ...(startTime ? { startTime } : {}),
    ...(endTime ? { endTime } : {}),
    ...(bytesOut !== undefined ? { bytesOut } : {}),
    ...(bytesIn !== undefined ? { bytesIn } : {}),
    raw: record
  };
}

function sourceLocator(record: Record<string, unknown>, index: number): string {
  const flowId = firstString(record, ["flowId", "flow_id", "uid"]);
  return flowId ? `flow:${flowId}` : `flows[${index}]`;
}

function fiveTupleHash(flow: FlowSummary): string {
  return createHash("sha256").update(fiveTuple(flow)).digest("hex");
}

function fiveTuple(flow: FlowSummary): string {
  return [
    flow.srcIp.toLowerCase(),
    portTuplePart(flow.srcPort),
    (flow.destIp ?? flow.destDomain ?? "").toLowerCase(),
    portTuplePart(flow.destPort),
    flow.protocol.toLowerCase()
  ].join("|");
}

function portTuplePart(value: number | undefined): string {
  return value === undefined ? "" : String(value);
}

function evidenceHashInput(flow: FlowSummary): Record<string, unknown> {
  return {
    sourceLocator: flow.sourceLocator,
    srcIp: flow.srcIp,
    srcPort: flow.srcPort,
    destIp: flow.destIp,
    destPort: flow.destPort,
    destDomain: flow.destDomain,
    protocol: flow.protocol,
    process: flow.process,
    user: flow.user,
    startTime: flow.startTime,
    endTime: flow.endTime,
    bytesOut: flow.bytesOut,
    bytesIn: flow.bytesIn,
    raw: flow.raw
  };
}

function destinationIps(text: string): Set<string> {
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

function destinationDomains(text: string): Set<string> {
  const domains = new Set<string>();
  for (const match of text.matchAll(/\b(?:[a-z0-9-]+\.)+[a-z]{2,}\b/giu)) {
    const domain = normalizeDomain(match[0]);
    if (domain && !looksLikeExecutable(domain)) {
      domains.add(domain);
    }
  }
  return domains;
}

function looksLikeExecutable(domain: string): boolean {
  return /\.(?:bat|cmd|dll|exe|js|msi|ps1|scr|sys|vbs)$/iu.test(domain);
}

function firstString(record: Record<string, unknown>, keys: readonly string[]): string | undefined {
  for (const key of keys) {
    const value = record[key];
    const stringValue = stringValueOf(value);
    if (stringValue) {
      return stringValue;
    }
  }
  return undefined;
}

function firstNumber(record: Record<string, unknown>, keys: readonly string[]): number | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    const stringValue = stringValueOf(value);
    if (!stringValue) {
      continue;
    }
    const number = Number(stringValue);
    if (Number.isFinite(number)) {
      return number;
    }
  }
  return undefined;
}

function stringValueOf(value: unknown): string | undefined {
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : undefined;
  }
  if (typeof value === "number" && Number.isFinite(value)) {
    return String(value);
  }
  if (Array.isArray(value)) {
    for (const entry of value) {
      const stringValue = stringValueOf(entry);
      if (stringValue) {
        return stringValue;
      }
    }
  }
  return undefined;
}

function normalizeDomain(value: string | undefined): string | undefined {
  const normalized = value?.trim().toLowerCase().replace(/\.$/u, "");
  return normalized && normalized.includes(".") ? normalized : undefined;
}

function normalizeProtocol(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  const lower = value.toLowerCase();
  if (lower.includes("tcp")) {
    return "tcp";
  }
  if (lower.includes("udp")) {
    return "udp";
  }
  if (lower.includes("icmp")) {
    return "icmp";
  }
  return lower;
}

function inferProtocol(record: Record<string, unknown>): string {
  if (firstString(record, ["tcp.srcport", "tcp.dstport"])) {
    return "tcp";
  }
  if (firstString(record, ["udp.srcport", "udp.dstport"])) {
    return "udp";
  }
  return "unknown";
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return Boolean(input) && typeof input === "object" && !Array.isArray(input);
}
