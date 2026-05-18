import { createFakeAdapter } from "./fake-adapter.js";
import type { FakeAdapter } from "./fake-adapter.js";
import type { AdapterMetadata } from "./types.js";

export const fakeAdapterMetadata = [
  {
    id: "adapter.gmail.fake",
    kind: "gmail",
    displayName: "Fake Gmail",
    capabilities: ["message.read", "message.send", "thread.label"],
    live: false
  },
  {
    id: "adapter.sheets.fake",
    kind: "sheets",
    displayName: "Fake Google Sheets",
    capabilities: ["sheet.read", "sheet.append", "sheet.update"],
    live: false
  },
  {
    id: "adapter.email.fake",
    kind: "email",
    displayName: "Fake SMTP Email",
    capabilities: ["email.send"],
    live: false
  },
  {
    id: "adapter.whatsapp.fake",
    kind: "whatsapp",
    displayName: "Fake WhatsApp",
    capabilities: ["message.send"],
    live: false
  },
  {
    id: "adapter.telegram.fake",
    kind: "telegram",
    displayName: "Fake Telegram",
    capabilities: ["message.send"],
    live: false
  }
] as const satisfies readonly AdapterMetadata[];

export function createDefaultFakeAdapters(): Map<string, FakeAdapter> {
  return new Map<string, FakeAdapter>(
    fakeAdapterMetadata.map((metadata) => [metadata.id, createFakeAdapter(metadata)])
  );
}

export function requireFakeAdapter(adapterId: string, adapters = createDefaultFakeAdapters()) {
  const adapter = adapters.get(adapterId);
  if (!adapter) {
    throw new Error(`Unknown fake adapter '${adapterId}'.`);
  }

  return adapter;
}
