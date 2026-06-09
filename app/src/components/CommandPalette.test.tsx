import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { CommandPalette } from "./CommandPalette";
import { DEFAULT_SETTINGS, type Session, useSessionStore } from "../lib/sessions";
import {
  clearTerminalRenderProfiles,
  recordTerminalRenderProfile,
} from "../lib/terminal-render-profile";
import { COMMAND_PALETTE_USED_KEY } from "../lib/command-palette-discovery";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    terminalLayout: null,
    activeTerminalPaneId: null,
    maximizedTerminalPaneId: null,
    arrangements: [],
    activeSidebarView: "files",
    workspaces: [],
    selectedFile: null,
    sessionEvents: [],
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue([]);
  globalThis.__TAURI_MOCKS__.dialogConfirm.mockReset();
  globalThis.__TAURI_MOCKS__.dialogConfirm.mockResolvedValue(true);
  globalThis.__TAURI_MOCKS__.updateCheck.mockReset();
  globalThis.__TAURI_MOCKS__.updateCheck.mockResolvedValue(null);
  vi.mocked(window.confirm).mockReset();
  vi.mocked(window.confirm).mockReturnValue(true);
  vi.mocked(window.prompt).mockReset();
  vi.mocked(window.prompt).mockReturnValue(null);
  clearTerminalRenderProfiles();
  localStorage.removeItem("onibiTerminalDebug");
  localStorage.removeItem(COMMAND_PALETTE_USED_KEY);
  localStorage.removeItem("onibi.token");
  localStorage.removeItem("onibi.port");
}

describe("CommandPalette", () => {
  beforeEach(() => {
    resetStore();
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test("opens with ctrl+p and executes a settings command", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "theme light" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().settings.theme).toBe("vscode-light-plus");
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
    expect(localStorage.getItem(COMMAND_PALETTE_USED_KEY)).toBe("1");
  });

  test("switches to a matching session command", () => {
    const sessions: Session[] = [
      {
        id: "pty-1",
        agent: "shell",
        workspaceId: "workspace:/repo",
        title: "Shell",
        status: "running",
        createdAt: 1,
        pendingApprovals: [],
      },
      {
        id: "pty-2",
        agent: "gemini",
        workspaceId: "workspace:/repo",
        title: "Gemini",
        status: "running",
        createdAt: 2,
        pendingApprovals: [],
      },
    ];
    useSessionStore.setState({
      sessions,
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", metaKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "switch gemini" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
  });

  test("supports keyboard navigation and escape", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Escape" });

    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
  });

  test("focuses attention items and clears active attention", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-ok",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
        },
        {
          id: "pty-attention",
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex",
          status: "running",
          createdAt: Date.now(),
          pendingApprovals: [],
          lastTrigger: {
            id: "tests-failed",
            label: "Tests failed",
            pattern: "failed",
            line: "tests failed",
            actions: ["badge"],
            timestamp: Date.now(),
          },
        },
      ],
      activeSessionId: "pty-ok",
      terminalLayout: {
        type: "split",
        paneId: "split-1",
        direction: "vertical",
        children: [
          { type: "leaf", paneId: "pane-ok", sessionId: "pty-ok" },
          { type: "leaf", paneId: "pane-attention", sessionId: "pty-attention" },
        ],
      },
      activeTerminalPaneId: "pane-ok",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    let input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "focus next attention" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSessionId).toBe("pty-attention");

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "clear active session attention" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().sessions[1].lastTrigger).toBeNull();
  });

  test("switches the sidebar to workspace search from the palette", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "workspace search" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().activeSidebarView).toBe("search");
  });

  test("opens the quick workspace launcher", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo/pkg",
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "quick workspace launcher" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(screen.getByRole("dialog", { name: "New Session" })).toBeTruthy();
  });

  test("opens the remote SSH launcher", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "remote ssh" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(
      screen.getByRole("dialog", { name: "New Remote SSH Session" }),
    ).toBeTruthy();
  });

  test("sends desktop remote input through pane HTTP routes", async () => {
    localStorage.setItem("onibi.token", "test-token");
    localStorage.setItem("onibi.port", "17893");
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({
            protocol_version: "1.0",
            targets: [
              {
                paneId: "pty-1",
                sessionId: "pty-1",
                label: "Shell",
                agent: "shell",
                workspaceId: "workspace:/repo",
                cwd: "/repo",
                status: "running",
                trustMode: "full-access",
              },
            ],
          }),
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
            bytes: 6,
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

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "send text active pane" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(
      await screen.findByRole("dialog", { name: "Remote Pane Input" }),
    ).toBeTruthy();
    await screen.findByText("Shell - running - full-access");
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
    expect(postCall).toBeTruthy();
    const request = postCall![1] as RequestInit;
    expect(JSON.parse(request.body as string)).toEqual({
      protocol_version: "1.0",
      text: "hello",
      sendEnter: true,
      confirmed: false,
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "pty_write",
      expect.anything(),
    );
  });

  test("confirms approval-required desktop remote input before dispatch", async () => {
    localStorage.setItem("onibi.token", "test-token");
    localStorage.setItem("onibi.port", "17893");
    const fetchMock = vi.fn(async (url: string, _init?: RequestInit) => {
      if (url.endsWith("/v1/panes/targets")) {
        return new Response(
          JSON.stringify({
            protocol_version: "1.0",
            targets: [
              {
                paneId: "pty-approval",
                sessionId: "pty-approval",
                label: "Claude",
                agent: "claude-code",
                workspaceId: "workspace:/repo",
                cwd: "/repo",
                status: "blocked",
                trustMode: "approval-required",
              },
            ],
          }),
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
            bytes: 9,
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

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "remote input" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await screen.findByText("Claude - blocked - approval-required");
    fireEvent.change(screen.getByLabelText("Text to send"), {
      target: { value: "continue" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(globalThis.__TAURI_MOCKS__.dialogConfirm).toHaveBeenCalled());
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

  test("dispatches update checks from the palette", () => {
    const listener = vi.fn();
    window.addEventListener("onibi:check-updates", listener);
    try {
      render(<CommandPalette />);
      fireEvent.keyDown(window, { key: "p", ctrlKey: true });
      const input = screen.getByLabelText("Search commands");
      fireEvent.change(input, { target: { value: "check updates" } });
      fireEvent.keyDown(input, { key: "Enter" });

      expect(listener).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener("onibi:check-updates", listener);
    }
  });

  test("bootstraps the active remote ssh session from the palette", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-remote",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "SSH",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          remote: {
            kind: "ssh",
            target: "alice@example.com",
            user: "alice",
            host: "example.com",
            keybindingPolicy: "local",
          },
        },
      ],
      activeSessionId: "pty-remote",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    const listener = vi.fn();
    window.addEventListener("onibi:terminal-notice", listener);
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "remote_ssh_bootstrap") {
        return {
          ok: true,
          target: "alice@example.com",
          helperPath: "/home/alice/.onibi/bin/onibi",
          helperVersion: "1.5.0-dev",
          stagingDir: "/home/alice/.onibi/staged",
          bootstrappedAt: 1234,
          stdout: "",
          stderr: "",
        };
      }
      return [];
    });

    try {
      render(<CommandPalette />);
      fireEvent.keyDown(window, { key: "p", ctrlKey: true });
      const input = screen.getByLabelText("Search commands");
      fireEvent.change(input, { target: { value: "bootstrap active remote" } });
      fireEvent.keyDown(input, { key: "Enter" });

      await waitFor(() => {
        expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
          "remote_ssh_bootstrap",
          {
            req: expect.objectContaining({
              target: "alice@example.com",
              user: "alice",
              host: "example.com",
            }),
          },
        );
      });
      expect(useSessionStore.getState().sessions[0].remote).toMatchObject({
        bootstrapStatus: "ready",
        stagingDir: "/home/alice/.onibi/staged",
      });
      expect(listener).toHaveBeenCalled();
    } finally {
      window.removeEventListener("onibi:terminal-notice", listener);
    }
  });

  test("starts the active remote ssh daemon from the palette", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-remote",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "SSH",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          remote: {
            kind: "ssh",
            target: "alice@example.com",
            user: "alice",
            host: "example.com",
            keybindingPolicy: "local",
            bootstrapStatus: "ready",
            helperPath: "/home/alice/.onibi/bin/onibi",
          },
        },
      ],
      activeSessionId: "pty-remote",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    const listener = vi.fn();
    window.addEventListener("onibi:terminal-notice", listener);
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, unknown>) => {
        if (command === "remote_ssh_daemon") {
          expect(args.req).toMatchObject({
            target: "alice@example.com",
            user: "alice",
            host: "example.com",
            helperPath: "/home/alice/.onibi/bin/onibi",
          });
          return {
            ok: true,
            target: "alice@example.com",
            helperPath: "/home/alice/.onibi/bin/onibi",
            runDir: "/home/alice/.onibi/run",
            pid: 4242,
            status: "started",
            logPath: "/home/alice/.onibi/run/onibi.log",
            startedAt: 2345,
            stdout: "",
            stderr: "",
          };
        }
        return [];
      },
    );

    try {
      render(<CommandPalette />);
      fireEvent.keyDown(window, { key: "p", ctrlKey: true });
      const input = screen.getByLabelText("Search commands");
      fireEvent.change(input, { target: { value: "start remote daemon" } });
      fireEvent.keyDown(input, { key: "Enter" });

      await waitFor(() => {
        expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
          "remote_ssh_daemon",
          {
            req: expect.objectContaining({
              target: "alice@example.com",
              helperPath: "/home/alice/.onibi/bin/onibi",
            }),
          },
        );
      });
      await waitFor(() => {
        expect(listener).toHaveBeenCalledWith(
          expect.objectContaining({
            detail: expect.objectContaining({
              title: "Remote Onibi daemon running",
              body: "pid 4242 · /home/alice/.onibi/run/onibi.log",
            }),
          }),
        );
      });
    } finally {
      window.removeEventListener("onibi:terminal-notice", listener);
    }
  });

  test("stages and pastes a clipboard image for the active remote ssh session", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-remote",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "SSH",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          remote: {
            kind: "ssh",
            target: "alice@example.com",
            user: "alice",
            host: "example.com",
            keybindingPolicy: "local",
            stagingDir: "/home/alice/.onibi/staged",
          },
        },
      ],
      activeSessionId: "pty-remote",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, unknown>) => {
        if (command === "clipboard_read_image_png") {
          return [137, 80, 78, 71];
        }
        if (command === "remote_ssh_stage_file") {
          expect(args.req).toMatchObject({
            target: "alice@example.com",
            user: "alice",
            host: "example.com",
            stagingDir: "/home/alice/.onibi/staged",
            data: [137, 80, 78, 71],
          });
          return {
            ok: true,
            remotePath: "/home/alice/.onibi/staged/image.png",
            bytes: 4,
            stdout: "",
            stderr: "",
          };
        }
        return [];
      },
    );

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "paste clipboard image remote" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_write", {
        id: "pty-remote",
        data: Array.from(new TextEncoder().encode("/home/alice/.onibi/staged/image.png")),
      });
    });
  });

  test("opens a git worktree from the palette", async () => {
    useSessionStore.setState({
      activeWorkspaceId: "workspace:/repo",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: { path?: string }) => {
        if (command === "git_worktrees") {
          return [
            {
              path: "/repo-feature",
              branch: "feature/mobile",
              head: null,
              detached: false,
              bare: false,
              prunable: false,
            },
          ];
        }
        if (command === "fs_workspace_info") {
          return { path: args.path, name: "repo-feature" };
        }
        return [];
      },
    );

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", metaKey: true });

    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "open worktree feature" } });
    expect(await screen.findByText("Open Worktree: feature/mobile")).toBeTruthy();
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(useSessionStore.getState().activeWorkspaceId).toBe(
        "workspace:/repo-feature",
      );
    });
  });

  test("hands off to another agent as a tab in the active pane", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-codex";
      }
      return null;
    });
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "claude-code",
          workspaceId: "workspace:/repo",
          title: "Claude · repo",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "handoff codex" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-codex");
    });
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-codex",
      sessionIds: ["pty-1", "pty-codex"],
    });
  });

  test("saves and restores terminal arrangements from the palette", async () => {
    vi.mocked(window.prompt).mockReturnValue("Pairing layout");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-restored";
      }
      return null;
    });
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
          restart: {
            command: "",
            args: [],
            cwd: "/repo",
            env: [["ONIBI_SHELL_INTEGRATION", "1"]],
            shellMode: "login",
          },
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      settings: { ...DEFAULT_SETTINGS, terminalConfirmClose: false },
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    let input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "save arrangement" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().arrangements[0]?.name).toBe("Pairing layout");

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "restore pairing" } });
    fireEvent.keyDown(input, { key: "Enter" });

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-restored");
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
      req: expect.objectContaining({
        command: "",
        shellMode: "login",
      }),
    });
    expect(useSessionStore.getState().terminalLayout).toEqual({
      type: "leaf",
      paneId: "pane-1",
      sessionId: "pty-restored",
      sessionIds: ["pty-restored"],
    });
  });

  test("switches workspace tabs with indexed prefix bindings", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "One",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-2",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Two",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      workspaceTabs: [
        {
          id: "tab-1",
          workspaceId: "workspace:/repo",
          title: "One",
          terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
          activeTerminalPaneId: "pane-1",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
        {
          id: "tab-2",
          workspaceId: "workspace:/repo",
          title: "Two",
          terminalLayout: { type: "leaf", paneId: "pane-2", sessionId: "pty-2" },
          activeTerminalPaneId: "pane-2",
          maximizedTerminalPaneId: null,
          createdAt: 1,
          updatedAt: 1,
        },
      ],
      activeWorkspaceId: "workspace:/repo",
      activeWorkspaceTabId: "tab-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      activeSessionId: "pty-1",
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "b", ctrlKey: true });
    fireEvent.keyDown(window, { key: "2" });

    expect(useSessionStore.getState().activeWorkspaceTabId).toBe("tab-2");
    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
  });

  test("opens the workspace navigator with prefix w", () => {
    useSessionStore.setState({
      workspaces: [
        { id: "workspace:/repo", path: "/repo", name: "repo" },
        { id: "workspace:/other", path: "/other", name: "other" },
      ],
      activeWorkspaceId: "workspace:/repo",
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "b", ctrlKey: true });
    fireEvent.keyDown(window, { key: "w" });

    expect(screen.getByRole("dialog", { name: "Workspace navigator" })).toBeTruthy();
    const otherWorkspace = screen.getByText("other").closest("button");
    expect(otherWorkspace).toBeTruthy();
    fireEvent.click(otherWorkspace!);

    expect(useSessionStore.getState().activeWorkspaceId).toBe("workspace:/other");
    expect(screen.queryByRole("dialog", { name: "Workspace navigator" })).toBeNull();
  });

  test("opens the session navigator with prefix g", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "pty-2",
          agent: "gemini",
          workspaceId: "workspace:/repo",
          title: "Gemini",
          status: "running",
          createdAt: 2,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "b", ctrlKey: true });
    fireEvent.keyDown(window, { key: "g" });

    expect(screen.getByRole("dialog", { name: "Session navigator" })).toBeTruthy();
    const geminiSession = screen.getByText("Gemini").closest("button");
    expect(geminiSession).toBeTruthy();
    fireEvent.click(geminiSession!);

    expect(useSessionStore.getState().activeSessionId).toBe("pty-2");
    expect(screen.queryByRole("dialog", { name: "Session navigator" })).toBeNull();
  });

  test("opens keybinding help with prefix question mark", () => {
    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "b", ctrlKey: true });
    fireEvent.keyDown(window, { key: "?", shiftKey: true });

    expect(screen.getByRole("dialog", { name: "Keybinding help" })).toBeTruthy();
    expect(screen.getByText("Open workspace navigator")).toBeTruthy();
    expect(screen.getByText("Open session navigator")).toBeTruthy();
  });

  test("enters terminal copy mode with prefix left bracket", () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    const listener = vi.fn();
    window.addEventListener("onibi:terminal-copy-mode", listener);

    try {
      render(<CommandPalette />);
      fireEvent.keyDown(window, { key: "b", ctrlKey: true });
      fireEvent.keyDown(window, { key: "[" });

      expect(listener).toHaveBeenCalled();
      expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({
        ptyId: "pty-1",
      });
    } finally {
      window.removeEventListener("onibi:terminal-copy-mode", listener);
    }
  });

  test("sends custom command keybindings to the active pane", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      terminalLayout: { type: "leaf", paneId: "pane-1", sessionId: "pty-1" },
      activeTerminalPaneId: "pane-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      settings: {
        ...DEFAULT_SETTINGS,
        customCommandKeybindings: [
          { keys: "prefix+t", command: "pnpm test", description: "Run tests" },
        ],
      },
    });

    render(<CommandPalette />);
    fireEvent.keyDown(window, { key: "b", ctrlKey: true });
    fireEvent.keyDown(window, { key: "t" });

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_write", {
        id: "pty-1",
        data: Array.from(new TextEncoder().encode("pnpm test\r")),
      });
    });
  });

  test("copies the active terminal render profile", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-1",
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Shell",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "pty-1",
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    });
    localStorage.setItem("onibiTerminalDebug", "1");
    const handleRequest = (event: Event) => {
      expect((event as CustomEvent<{ ptyId?: string }>).detail?.ptyId).toBe("pty-1");
      recordTerminalRenderProfile({
        ptyId: "pty-1",
        renderer: "webgl",
        inlineImageMode: "auto",
        rows: 24,
        cols: 80,
        bytes: 2048,
        chunks: 4,
        batches: 2,
        maxBatchBytes: 1024,
        avgFlushLatencyMs: 8,
        maxFlushLatencyMs: 12,
        replayBytes: 128,
        replayDurationMs: 5,
        totalDurationMs: 40,
        capturedAt: 1,
        reason: "request",
      });
    };
    window.addEventListener("onibi:terminal-render-profile-request", handleRequest);

    try {
      render(<CommandPalette />);
      fireEvent.keyDown(window, { key: "p", ctrlKey: true });
      const input = screen.getByLabelText("Search commands");
      fireEvent.change(input, { target: { value: "copy terminal render profile" } });
      fireEvent.keyDown(input, { key: "Enter" });

      await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
      expect(vi.mocked(navigator.clipboard.writeText).mock.calls[0][0]).toContain(
        '"ptyId": "pty-1"',
      );
      expect(vi.mocked(navigator.clipboard.writeText).mock.calls[0][0]).toContain(
        '"inlineImageMode": "auto"',
      );
    } finally {
      window.removeEventListener(
        "onibi:terminal-render-profile-request",
        handleRequest,
      );
    }
  });
});
