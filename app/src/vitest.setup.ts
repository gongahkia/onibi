import { vi, type Mock } from "vitest";

type TauriMocks = {
  invoke: Mock;
  listen: Mock;
  unlisten: Mock;
};

const tauriMocks: TauriMocks = {
  invoke: vi.fn(),
  listen: vi.fn(async () => tauriMocks.unlisten),
  unlisten: vi.fn(),
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

vi.mock("@tauri-apps/api/core", () => ({
  invoke: tauriMocks.invoke,
}));

vi.mock("@tauri-apps/api/event", () => ({
  listen: tauriMocks.listen,
}));
