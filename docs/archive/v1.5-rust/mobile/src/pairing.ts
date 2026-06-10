import type { ClientScope, Connection, PairingPayload } from "./types";
import { normalizeBaseUrl } from "./utils";

export function parsePairingInput(raw: string): PairingPayload {
  const trimmed = raw.trim();
  if (!trimmed) {
    throw new Error("Pairing payload is empty.");
  }
  if (trimmed.startsWith("onibi://")) {
    const url = new URL(trimmed);
    const payload = url.searchParams.get("payload") ?? url.searchParams.get("data");
    if (payload) {
      return parsePairingInput(decodeURIComponent(payload));
    }
    const token = url.searchParams.get("token");
    const baseUrl = url.searchParams.get("baseUrl") ?? url.searchParams.get("url");
    if (token && baseUrl) {
      return {
        token,
        scope: (url.searchParams.get("scope") as ClientScope | null) ?? undefined,
        machineId: url.searchParams.get("machineId") ?? undefined,
        vapidPublicKey: url.searchParams.get("vapidPublicKey") ?? undefined,
        transports: [{ name: "deep-link", url: baseUrl }],
      };
    }
  }
  try {
    const parsed = JSON.parse(trimmed) as PairingPayload;
    if (!parsed.token) {
      throw new Error("Pairing payload is missing token.");
    }
    return parsed;
  } catch (jsonError) {
    try {
      return parsePairingInput(atob(trimmed));
    } catch {
      throw jsonError instanceof Error ? jsonError : new Error("Invalid pairing payload.");
    }
  }
}

export function chooseBaseUrl(payload: PairingPayload): string {
  const transport =
    payload.transports?.find((item) => phoneReachableUrl(item.url)) ??
    payload.transports?.[0];
  if (transport?.url) {
    return normalizeBaseUrl(transport.url);
  }
  if (payload.host && payload.port) {
    return normalizeBaseUrl(`http://${payload.host}:${payload.port}`);
  }
  throw new Error("Pairing payload has no reachable transport.");
}

export function phoneReachableUrl(url: string): boolean {
  return !/\/\/(127\.0\.0\.1|localhost|\[::1\])(?::|\/|$)/.test(url);
}

export function uniqueStrings(items: string[]): string[] {
  return [...new Set(items.filter(Boolean).map(normalizeBaseUrl))];
}

export function candidateBaseUrls(connection: Connection): string[] {
  return uniqueStrings([
    connection.baseUrl,
    ...connection.transports
      .filter((transport) => phoneReachableUrl(transport.url))
      .map((transport) => normalizeBaseUrl(transport.url)),
    ...connection.transports.map((transport) => normalizeBaseUrl(transport.url)),
  ]);
}

export function nextBaseUrl(current: string, candidates: string[]): string {
  const normalized = normalizeBaseUrl(current);
  const list = uniqueStrings(candidates);
  if (list.length <= 1) {
    return normalized;
  }
  const index = list.indexOf(normalized);
  return list[(index + 1 + list.length) % list.length] ?? normalized;
}

export function transportLabelForUrl(connection: Connection, baseUrl: string): string {
  const normalized = normalizeBaseUrl(baseUrl);
  const transport = connection.transports.find(
    (item) => normalizeBaseUrl(item.url) === normalized,
  );
  return transport ? `${transport.name} ${normalized}` : normalized;
}
