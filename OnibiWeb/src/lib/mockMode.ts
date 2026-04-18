// MOCK_MODE — temporary dev-only helper. Remove this file and all `mockMode` references before shipping.
import type {
  ConnectionConfig,
  ControllableSessionSnapshot,
  SessionOutputChunk
} from "../types";

export function isMockMode(): boolean {
  try {
    const params = new URLSearchParams(window.location.search);
    if (params.get("mock") === "1") {
      window.sessionStorage.setItem("onibi-web.mock", "1");
      return true;
    }
    return window.sessionStorage.getItem("onibi-web.mock") === "1";
  } catch {
    return false;
  }
}

export function mockSearchSuffix(): string {
  return isMockMode() ? "?mock=1" : "";
}

export const MOCK_CONNECTION: ConnectionConfig = {
  baseURL: "http://mock.local:8787",
  token: "mock-token-0000-0000-0000"
};

export function mockSessions(): ControllableSessionSnapshot[] {
  const now = Date.now();
  return [
    {
      id: "mock-sess-1",
      displayName: "zsh · ~/onibi",
      startedAt: new Date(now - 1_800_000).toISOString(),
      lastActivityAt: new Date(now - 4_000).toISOString(),
      status: "running",
      isControllable: true,
      workingDirectory: "/Users/you/Desktop/coding/projects/onibi",
      lastCommandPreview: "make run",
      bufferCursor: "mock-cursor-1"
    },
    {
      id: "mock-sess-2",
      displayName: "nvim · styles.css",
      startedAt: new Date(now - 900_000).toISOString(),
      lastActivityAt: new Date(now - 60_000).toISOString(),
      status: "running",
      isControllable: true,
      workingDirectory: "/Users/you/Desktop/coding/projects/onibi/OnibiWeb",
      lastCommandPreview: "nvim src/styles.css",
      bufferCursor: "mock-cursor-2"
    },
    {
      id: "mock-sess-3",
      displayName: "node · vite",
      startedAt: new Date(now - 300_000).toISOString(),
      lastActivityAt: new Date(now - 120_000).toISOString(),
      status: "exited",
      isControllable: false,
      workingDirectory: "/Users/you/Desktop/coding/projects/onibi/OnibiWeb",
      lastCommandPreview: "npm run dev",
      bufferCursor: null
    }
  ];
}

export function mockBufferChunks(sessionId: string): SessionOutputChunk[] {
  const base = Date.now() - 5_000;
  const lines = [
    "$ make run",
    "→ swift build --configuration debug",
    "Compiling OnibiCore…",
    "Compiling Onibi…",
    "Build complete! (3.42s)",
    "→ launching Onibi.app",
    "[gateway] listening on 127.0.0.1:8787",
    "[proxy]   control.sock ready",
    "[ghostty] session attached: mock-sess-1",
    "$ "
  ];
  return lines.map((text, index) => ({
    id: `${sessionId}-chunk-${index}`,
    sessionId,
    stream: "stdout",
    timestamp: new Date(base + index * 400).toISOString(),
    data: text + "\n"
  }));
}

export function mockEchoChunk(sessionId: string, text: string): SessionOutputChunk {
  return {
    id: `${sessionId}-echo-${Date.now()}-${Math.random().toString(16).slice(2, 6)}`,
    sessionId,
    stream: "stdout",
    timestamp: new Date().toISOString(),
    data: text.endsWith("\n") ? text : text + "\n"
  };
}
