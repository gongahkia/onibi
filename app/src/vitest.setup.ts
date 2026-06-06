import { cleanup } from "@testing-library/react";
import { afterEach, vi, type Mock } from "vitest";

type TauriMocks = {
  dialogOpen: Mock;
  invoke: Mock;
  listen: Mock;
  openerRevealItemInDir: Mock;
  processRelaunch: Mock;
  updateCheck: Mock;
  unlisten: Mock;
};

const storeData = new Map<string, unknown>();

const tauriMocks: TauriMocks = {
  dialogOpen: vi.fn(),
  invoke: vi.fn(),
  listen: vi.fn(async () => tauriMocks.unlisten),
  openerRevealItemInDir: vi.fn(async () => undefined),
  processRelaunch: vi.fn(async () => undefined),
  updateCheck: vi.fn(async () => null),
  unlisten: vi.fn(),
};

const storeMock = {
  get: vi.fn(async (key: string) => storeData.get(key)),
  set: vi.fn(async (key: string, value: unknown) => {
    storeData.set(key, value);
  }),
  save: vi.fn(async () => undefined),
  has: vi.fn(async (key: string) => storeData.has(key)),
  delete: vi.fn(async (key: string) => storeData.delete(key)),
  clear: vi.fn(async () => {
    storeData.clear();
  }),
};

Object.defineProperty(globalThis, "__TAURI_MOCKS__", {
  value: tauriMocks,
  writable: false,
});

class ResizeObserverMock {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
}

Object.defineProperty(globalThis, "ResizeObserver", {
  value: ResizeObserverMock,
  writable: true,
});

Object.defineProperty(globalThis, "requestAnimationFrame", {
  value: (callback: FrameRequestCallback) => window.setTimeout(callback, 0),
  writable: true,
});

Object.defineProperty(globalThis, "cancelAnimationFrame", {
  value: (id: number) => window.clearTimeout(id),
  writable: true,
});

if (typeof Range !== "undefined") {
  Object.defineProperty(Range.prototype, "getBoundingClientRect", {
    value: () => new DOMRect(0, 0, 0, 0),
    writable: true,
  });
  Object.defineProperty(Range.prototype, "getClientRects", {
    value: () => ({
      length: 0,
      item: () => null,
      [Symbol.iterator]: function* iterator() {},
    }),
    writable: true,
  });
}

Object.defineProperty(window, "matchMedia", {
  value: vi.fn(() => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })),
  writable: true,
});

Object.defineProperty(window, "confirm", {
  value: vi.fn(() => true),
  writable: true,
});

Object.defineProperty(window, "prompt", {
  value: vi.fn(() => null),
  writable: true,
});

Object.defineProperty(navigator, "clipboard", {
  value: {
    readText: vi.fn(async () => ""),
    write: vi.fn(async () => undefined),
    writeText: vi.fn(async () => undefined),
  },
  writable: true,
});

class ClipboardItemMock {
  readonly items: Record<string, Blob>;

  constructor(items: Record<string, Blob>) {
    this.items = items;
  }
}

Object.defineProperty(globalThis, "ClipboardItem", {
  value: ClipboardItemMock,
  writable: true,
});

Object.defineProperty(URL, "createObjectURL", {
  value: vi.fn(() => "blob:onibi-test"),
  writable: true,
});

Object.defineProperty(URL, "revokeObjectURL", {
  value: vi.fn(),
  writable: true,
});

vi.mock("@tauri-apps/api/core", () => ({
  convertFileSrc: vi.fn((path: string) => `asset://${path}`),
  invoke: tauriMocks.invoke,
}));

vi.mock("@tauri-apps/api/event", () => ({
  listen: tauriMocks.listen,
}));

vi.mock("@tauri-apps/plugin-dialog", () => ({
  open: tauriMocks.dialogOpen,
}));

vi.mock("@tauri-apps/plugin-opener", () => ({
  revealItemInDir: tauriMocks.openerRevealItemInDir,
}));

vi.mock("@tauri-apps/plugin-process", () => ({
  relaunch: tauriMocks.processRelaunch,
}));

vi.mock("@tauri-apps/plugin-store", () => ({
  load: vi.fn(async () => storeMock),
  Store: {
    load: vi.fn(async () => storeMock),
  },
}));

vi.mock("@tauri-apps/plugin-updater", () => ({
  check: tauriMocks.updateCheck,
}));

afterEach(() => {
  cleanup();
});
