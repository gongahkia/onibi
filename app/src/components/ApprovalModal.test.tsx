import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ApprovalModal } from "./ApprovalModal";
import type { ApprovalPendingMessage } from "../lib/approval-client";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

const pending: ApprovalPendingMessage = {
  type: "approval-pending",
  protocol_version: "1.0",
  approval_id: "01HAPPROVAL0000000000000",
  machine_id: "01HMACHINE00000000000000",
  session_id: "01HSESSION00000000000000",
  agent: "claude-code",
  tool: "Bash",
  input: { command: "rm -rf node_modules" },
  cwd: "/tmp/onibi",
};

describe("ApprovalModal", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn(async () => ({ ok: true, status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    globalThis.__TAURI_MOCKS__.listen.mockReset();
    globalThis.__TAURI_MOCKS__.listen.mockResolvedValue(
      globalThis.__TAURI_MOCKS__.unlisten,
    );
    globalThis.__TAURI_MOCKS__.requestUserAttention.mockClear();
    useSessionStore.setState({
      hydrated: true,
      sessions: [],
      activeSessionId: null,
      selectedFile: null,
      settings: DEFAULT_SETTINGS,
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test("renders a pending approval and allows it", async () => {
    const onResolved = vi.fn();
    render(<ApprovalModal initialPending={pending} token="test-token" onResolved={onResolved} />);

    expect(screen.getByRole("dialog", { name: "Approval request" })).toBeTruthy();
    expect(screen.getByText("claude-code")).toBeTruthy();
    expect(screen.getByText("rm -rf node_modules")).toBeTruthy();
    expect(screen.getByText("Destructive delete")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/approval/01HAPPROVAL0000000000000/decide",
        expect.objectContaining({
          method: "POST",
          headers: expect.objectContaining({
            authorization: "Bearer test-token",
          }),
        }),
      );
    });
    expect(onResolved).toHaveBeenCalledWith("01HAPPROVAL0000000000000");
  });

  test("submits edited Bash input as updatedInput", async () => {
    render(<ApprovalModal initialPending={pending} token="test-token" />);
    fireEvent.click(screen.getByRole("button", { name: "Edit input" }));
    fireEvent.change(screen.getByLabelText("Edited tool input"), {
      target: { value: "echo skipped" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Approve edited command" }));

    await waitFor(() => {
      const [, request] = fetchMock.mock.calls[0];
      expect(JSON.parse(request.body as string)).toEqual({
        decision: "allow",
        updatedInput: { command: "echo skipped" },
      });
    });
  });

  test("submits custom deny reason", async () => {
    render(<ApprovalModal initialPending={pending} token="test-token" />);
    fireEvent.change(screen.getByLabelText("Deny reason"), {
      target: { value: "too broad" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));

    await waitFor(() => {
      const [, request] = fetchMock.mock.calls[0];
      expect(JSON.parse(request.body as string)).toEqual({
        decision: "deny",
        reason: "too broad",
      });
    });
  });

  test("shows queue position and timeout countdown", () => {
    render(
      <ApprovalModal
        initialPending={{
          ...pending,
          created_at: Date.now() - 1_000,
          expires_at: Date.now() + 30_000,
        }}
        token="test-token"
      />,
    );
    expect(screen.getByText(/denies in 0:/)).toBeTruthy();
  });

  test("shows matched policy metadata", () => {
    render(
      <ApprovalModal
        initialPending={{
          ...pending,
          metadata: {
            onibi_policy: {
              name: "destructive shell commands",
              decision: "always-ask",
              source: "manual",
            },
          },
        }}
        token="test-token"
      />,
    );

    expect(
      screen.getByText('Policy "destructive shell commands" · always-ask'),
    ).toBeTruthy();
  });

  test("requests informational attention once for a new realtime approval", async () => {
    const pulseListener = vi.fn();
    window.addEventListener("onibi:approval-attention", pulseListener);
    globalThis.__TAURI_MOCKS__.listen.mockImplementation(async (_event, handler) => {
      handler({ payload: pending });
      handler({ payload: pending });
      return globalThis.__TAURI_MOCKS__.unlisten;
    });

    try {
      render(<ApprovalModal token="test-token" />);

      await waitFor(() => {
        expect(globalThis.__TAURI_MOCKS__.requestUserAttention).toHaveBeenCalledTimes(1);
      });
      expect(pulseListener).toHaveBeenCalledTimes(1);
      expect(screen.getByRole("dialog", { name: "Approval request" })).toBeTruthy();
    } finally {
      window.removeEventListener("onibi:approval-attention", pulseListener);
    }
  });

  test("suppresses dock attention for the foreground approval session", async () => {
    useSessionStore.setState({
      activeSessionId: pending.session_id,
      selectedFile: null,
      settings: { ...DEFAULT_SETTINGS, suppressForegroundTabNotifications: true },
    });
    const pulseListener = vi.fn();
    window.addEventListener("onibi:approval-attention", pulseListener);
    globalThis.__TAURI_MOCKS__.listen.mockImplementation(async (_event, handler) => {
      handler({ payload: pending });
      return globalThis.__TAURI_MOCKS__.unlisten;
    });

    try {
      render(<ApprovalModal token="test-token" />);

      await waitFor(() => {
        expect(pulseListener).toHaveBeenCalledTimes(1);
      });
      expect(globalThis.__TAURI_MOCKS__.requestUserAttention).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener("onibi:approval-attention", pulseListener);
    }
  });

  test("renders Write tool payload as file additions", () => {
    render(
      <ApprovalModal
        initialPending={{
          ...pending,
          tool: "Write",
          input: { file_path: "/repo/app.ts", content: "export const ok = true;" },
        }}
        token="test-token"
      />,
    );

    expect(screen.getByLabelText("Write file change preview")).toBeTruthy();
    expect(screen.getByText("Write file")).toBeTruthy();
    expect(screen.getByText("/repo/app.ts")).toBeTruthy();
    expect(screen.getByText("+ export const ok = true;")).toBeTruthy();
  });

  test("renders Edit tool payload as a replacement diff", () => {
    render(
      <ApprovalModal
        initialPending={{
          ...pending,
          tool: "Edit",
          input: {
            file_path: "/repo/app.ts",
            old_string: "const unsafe = true;",
            new_string: "const unsafe = false;",
          },
        }}
        token="test-token"
      />,
    );

    expect(screen.getByText("Edit file")).toBeTruthy();
    expect(screen.getByText("- const unsafe = true;")).toBeTruthy();
    expect(screen.getByText("+ const unsafe = false;")).toBeTruthy();
  });

  test("renders MultiEdit payload as numbered edit diffs", () => {
    render(
      <ApprovalModal
        initialPending={{
          ...pending,
          tool: "MultiEdit",
          input: {
            file_path: "/repo/app.ts",
            edits: [
              { old_string: "alpha", new_string: "beta" },
              { old_string: "gamma", new_string: "delta" },
            ],
          },
        }}
        token="test-token"
      />,
    );

    expect(screen.getByText("Edit 1")).toBeTruthy();
    expect(screen.getByText("Edit 2")).toBeTruthy();
    expect(screen.getByText("- alpha")).toBeTruthy();
    expect(screen.getByText("+ delta")).toBeTruthy();
  });
});
