import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { runPaneCommand } from "./remote-keystrokes";

beforeEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("runPaneCommand posts to explicit pane run endpoint", async () => {
  window.localStorage.setItem("onibi.token", "test-token");
  window.localStorage.setItem("onibi.port", "17894");
  const fetchMock = vi.fn(async () =>
    Response.json({
      ok: true,
      protocol_version: "1.0",
      paneId: "pane-1",
      sessionId: "session-1",
      bytes: 10,
      auditId: "audit-1",
      trustMode: "full-access",
      requiresConfirmation: false,
      destructive: false,
      preset: null,
    }),
  );
  vi.stubGlobal("fetch", fetchMock);

  await runPaneCommand("pane-1", "echo ready", true);

  expect(fetchMock).toHaveBeenCalledWith(
    "http://127.0.0.1:17894/v1/panes/pane-1/run",
    expect.objectContaining({
      method: "POST",
      headers: expect.objectContaining({
        authorization: "Bearer test-token",
        "content-type": "application/json",
      }),
      body: JSON.stringify({
        protocol_version: "1.0",
        command: "echo ready",
        confirmed: true,
      }),
    }),
  );
});
