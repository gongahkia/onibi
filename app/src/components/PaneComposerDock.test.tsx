import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PaneComposerDock } from "./PaneComposerDock";

const fullAccessTarget = {
  paneId: "pty-1",
  sessionId: "pty-1",
  label: "Shell",
  agent: "shell",
  workspaceId: "workspace:/repo",
  cwd: "/repo",
  status: "running",
  trustMode: "full-access",
};

const approvalRequiredTarget = {
  paneId: "pty-approval",
  sessionId: "pty-approval",
  label: "Claude",
  agent: "claude-code",
  workspaceId: "workspace:/repo",
  cwd: "/repo",
  status: "blocked",
  trustMode: "approval-required",
};

function expand() {
  act(() => {
    window.dispatchEvent(new CustomEvent("onibi:open-pane-composer"));
  });
}

describe("PaneComposerDock", () => {
  beforeEach(() => {
    localStorage.setItem("onibi.token", "test-token");
    localStorage.setItem("onibi.port", "17893");
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
    localStorage.clear();
  });

  test("renders collapsed by default with an expand affordance", () => {
    render(<PaneComposerDock activeSessionId={null} />);
    expect(screen.getByLabelText("Open remote pane composer")).toBeTruthy();
    expect(screen.queryByLabelText("Target pane")).toBeNull();
  });

  test("expands when the onibi:open-pane-composer event fires", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();

    await waitFor(() => {
      expect(screen.getByLabelText("Target pane")).toBeTruthy();
    });
    await screen.findByText("Shell · running · full-access");
  });

  test("sends text through the pane HTTP route for full-access targets", async () => {
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      if (url.endsWith("/v1/panes/pty-1/send-text")) {
        expect(init?.headers).toMatchObject({ authorization: "Bearer test-token" });
        return new Response(
          JSON.stringify({
            ok: true,
            protocol_version: "1.0",
            paneId: "pty-1",
            sessionId: "pty-1",
            bytes: 5,
            auditId: "audit-1",
            trustMode: "full-access",
            requiresConfirmation: false,
            destructive: false,
            preset: null,
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();

    await screen.findByText("Shell · running · full-access");
    fireEvent.change(screen.getByLabelText("Text to send"), {
      target: { value: "hello" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/panes/pty-1/send-text",
        expect.objectContaining({ method: "POST" }),
      );
    });
    const postCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/v1/panes/pty-1/send-text"),
    );
    const request = postCall![1] as RequestInit;
    expect(JSON.parse(request.body as string)).toEqual({
      protocol_version: "1.0",
      text: "hello",
      sendEnter: true,
      confirmed: false,
    });
  });

  test("confirms approval-required dispatch via native confirm before sending", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [approvalRequiredTarget] }),
          { status: 200 },
        );
      }
      if (url.endsWith("/v1/panes/pty-approval/send-text")) {
        return new Response(
          JSON.stringify({
            ok: true,
            protocol_version: "1.0",
            paneId: "pty-approval",
            sessionId: "pty-approval",
            bytes: 8,
            auditId: "audit-2",
            trustMode: "approval-required",
            requiresConfirmation: false,
            destructive: false,
            preset: null,
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-approval" />);
    expand();

    await screen.findByText("Claude · blocked · approval-required");
    fireEvent.change(screen.getByLabelText("Text to send"), {
      target: { value: "continue" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(globalThis.__TAURI_MOCKS__.dialogConfirm).toHaveBeenCalled(),
    );
    const postCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/v1/panes/pty-approval/send-text"),
    );
    expect(postCall).toBeTruthy();
    const request = postCall![1] as RequestInit;
    expect(JSON.parse(request.body as string)).toMatchObject({
      text: "continue",
      confirmed: true,
    });
  });

  test("aborts the dispatch when the user declines the confirmation", async () => {
    globalThis.__TAURI_MOCKS__.dialogConfirm.mockResolvedValue(false);
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [approvalRequiredTarget] }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-approval" />);
    expand();
    await screen.findByText("Claude · blocked · approval-required");
    fireEvent.change(screen.getByLabelText("Text to send"), {
      target: { value: "continue" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(globalThis.__TAURI_MOCKS__.dialogConfirm).toHaveBeenCalled(),
    );
    const dispatched = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/v1/panes/pty-approval/send-text"),
    );
    expect(dispatched).toBeUndefined();
  });

  test("retries with confirmed=true after a 409 from the server", async () => {
    let sendCallCount = 0;
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      if (url.endsWith("/v1/panes/pty-1/send-text")) {
        sendCallCount++;
        if (sendCallCount === 1) {
          return new Response(
            JSON.stringify({ error: "requires confirmation" }),
            { status: 409 },
          );
        }
        return new Response(
          JSON.stringify({
            ok: true,
            protocol_version: "1.0",
            paneId: "pty-1",
            sessionId: "pty-1",
            bytes: 5,
            auditId: "audit-3",
            trustMode: "full-access",
            requiresConfirmation: false,
            destructive: false,
            preset: null,
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();
    await screen.findByText("Shell · running · full-access");
    fireEvent.change(screen.getByLabelText("Text to send"), {
      target: { value: "hello" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(sendCallCount).toBe(2));
    const calls = fetchMock.mock.calls.filter(([url]) =>
      String(url).endsWith("/v1/panes/pty-1/send-text"),
    );
    expect(JSON.parse(calls[0][1]!.body as string)).toMatchObject({ confirmed: false });
    expect(JSON.parse(calls[1][1]!.body as string)).toMatchObject({ confirmed: true });
  });

  test("dispatches the Continue preset via send-keys", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      if (url.endsWith("/v1/panes/pty-1/send-keys")) {
        return new Response(
          JSON.stringify({
            ok: true,
            protocol_version: "1.0",
            paneId: "pty-1",
            sessionId: "pty-1",
            bytes: 1,
            auditId: "audit-4",
            trustMode: "full-access",
            requiresConfirmation: false,
            destructive: false,
            preset: "continue",
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();
    await screen.findByText("Shell · running · full-access");
    fireEvent.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() => {
      const presetCall = fetchMock.mock.calls.find(([url]) =>
        String(url).endsWith("/v1/panes/pty-1/send-keys"),
      );
      expect(presetCall).toBeTruthy();
      expect(JSON.parse(presetCall![1]!.body as string)).toMatchObject({
        preset: "continue",
      });
    });
  });

  test("confirms a destructive preset (Interrupt) before sending", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      if (url.endsWith("/v1/panes/pty-1/send-keys")) {
        return new Response(
          JSON.stringify({
            ok: true,
            protocol_version: "1.0",
            paneId: "pty-1",
            sessionId: "pty-1",
            bytes: 1,
            auditId: "audit-5",
            trustMode: "full-access",
            requiresConfirmation: true,
            destructive: true,
            preset: "interrupt",
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();
    await screen.findByText("Shell · running · full-access");
    fireEvent.click(screen.getByRole("button", { name: "Interrupt" }));

    await waitFor(() =>
      expect(globalThis.__TAURI_MOCKS__.dialogConfirm).toHaveBeenCalled(),
    );
    const presetCall = fetchMock.mock.calls.find(([url]) =>
      String(url).endsWith("/v1/panes/pty-1/send-keys"),
    );
    expect(presetCall).toBeTruthy();
    expect(JSON.parse(presetCall![1]!.body as string)).toMatchObject({
      preset: "interrupt",
      confirmed: true,
    });
  });

  test("collapses again when the chevron is clicked", async () => {
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({ protocol_version: "1.0", targets: [fullAccessTarget] }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    vi.stubGlobal("fetch", fetchMock);
    render(<PaneComposerDock activeSessionId="pty-1" />);
    expand();
    await screen.findByText("Shell · running · full-access");
    fireEvent.click(screen.getByLabelText("Collapse remote pane composer"));
    expect(screen.getByLabelText("Open remote pane composer")).toBeTruthy();
  });
});
