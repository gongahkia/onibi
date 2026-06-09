export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function normalizeBaseUrl(url: string): string {
  return url.replace(/\/+$/, "");
}

export function storageAvailable(): boolean {
  return typeof window !== "undefined" && "localStorage" in window;
}

export function shortMachineId(machineId: string): string {
  return machineId.length > 18 ? `${machineId.slice(0, 8)}...${machineId.slice(-6)}` : machineId;
}

export function decodeBase64Text(data: string): string {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new TextDecoder().decode(bytes);
}

export function urlBase64ToUint8Array(value: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (value.length % 4)) % 4);
  const base64 = `${value}${padding}`.replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const output = new Uint8Array(new ArrayBuffer(raw.length));
  for (let index = 0; index < raw.length; index += 1) {
    output[index] = raw.charCodeAt(index);
  }
  return output;
}

export function formatTime(value: number): string {
  const date = value > 10_000_000_000 ? new Date(value) : new Date(value * 1000);
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
