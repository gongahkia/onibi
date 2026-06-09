import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { NewSessionDialog } from "./NewSessionDialog";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

let fetchMock: ReturnType<typeof vi.fn>;

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
    workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    selectedFile: null,
    sessionEvents: [],
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.dialogOpen.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  fetchMock = vi.fn(async (url: string) => {
    if (url.includes("/v1/config/status")) {
      return new Response(
        JSON.stringify({
          adapters: {
            claude: { transport: "hook", acpCommand: "claude-agent-acp", acpArgs: [] },
            hermes: { transport: "acp", acpCommand: "hermes", acpArgs: ["acp"] },
          },
          policyValidation: { ok: true },
        }),
        { status: 200 },
      );
    }
    if (url.includes("/v1/adapters/hermes/acp/prompt")) {
      return new Response(
        JSON.stringify({
          protocol_version: "1.0",
          sessionId: "hermes-session-1",
          stopReason: "end_turn",
        }),
        { status: 200 },
      );
    }
    return new Response("not found", { status: 404 });
  });
  vi.stubGlobal("fetch", fetchMock);
}

describe("NewSessionDialog", () => {
  beforeEach(() => {
    resetStore();
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  test("spawns a shell session in the selected workspace", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "pty_spawn") {
        return "pty-shell";
      }
      return null;
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    fireEvent.change(screen.getByLabelText("Agent"), {
      target: { value: "shell" },
    });
    fireEvent.click(screen.getByText("Start"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
        req: {
          command: "",
          args: [],
          cwd: "/repo",
          env: [["ONIBI_SHELL_INTEGRATION", "1"]],
          shellMode: "auto",
          rows: 30,
          cols: 100,
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Plain shell · repo",
          trustMode: "approval-required",
        },
      });
    });
    expect(useSessionStore.getState().activeSessionId).toBe("pty-shell");
  });

  test("chooses a workspace with the native folder picker", async () => {
    globalThis.__TAURI_MOCKS__.dialogOpen.mockResolvedValue("/Users/me/project");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_workspace_info") {
        return { path: "/Users/me/project", name: "project" };
      }
      return null;
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    fireEvent.click(screen.getByText("Choose Folder"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.dialogOpen).toHaveBeenCalledWith({
        directory: true,
        multiple: false,
        title: "Choose workspace folder",
      });
    });

    expect(await screen.findByText("/Users/me/project")).toBeTruthy();
    expect(useSessionStore.getState().workspaces).toContainEqual({
      id: "workspace:/Users/me/project",
      path: "/Users/me/project",
      name: "project",
    });
  });

  test("uses the default agent while still requiring an explicit workspace", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_resolve_binary") {
        return "/usr/local/bin/codex";
      }
      if (command === "pty_spawn") {
        return "pty-codex";
      }
      return null;
    });
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "codex",
        agentCommands: {
          ...DEFAULT_SETTINGS.agentCommands,
          codex: "codex --model gpt-5",
        },
      },
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    expect((screen.getByLabelText("Agent") as HTMLSelectElement).value).toBe("codex");
    await waitFor(() => {
      expect(screen.getByText(/usr\/local\/bin\/codex/)).toBeTruthy();
    });
    fireEvent.change(screen.getByTestId("Initial prompt-plain-text"), {
      target: { value: "review this repo" },
    });
    fireEvent.click(screen.getByText("Start"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("pty_spawn", {
        req: {
          command: "codex",
          args: [
            "--model",
            "gpt-5",
            "review this repo",
          ],
          cwd: "/repo",
          env: [],
          rows: 30,
          cols: 100,
          agent: "codex",
          workspaceId: "workspace:/repo",
          title: "Codex · repo",
          trustMode: "approval-required",
        },
      });
    });
    expect(useSessionStore.getState().activeSessionId).toBe("pty-codex");
  });

  test("reuses an existing live session for the same agent and workspace", async () => {
    const onClose = vi.fn();
    useSessionStore.setState({
      sessions: [
        {
          id: "pty-existing",
          agent: "claude-code",
          workspaceId: "workspace:/repo",
          title: "Claude Code · repo",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
          lastExitCode: null,
          lastTrigger: null,
          lastCommandBlockId: null,
          transcript: null,
          restart: null,
          remote: null,
        },
      ],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_resolve_binary") {
        return "/usr/local/bin/claude";
      }
      if (command === "pty_spawn") {
        return "pty-new";
      }
      return null;
    });

    render(<NewSessionDialog open onClose={onClose} />);
    await waitFor(() => {
      expect(screen.getByText(/usr\/local\/bin\/claude/)).toBeTruthy();
    });
    fireEvent.click(screen.getByText("Start"));

    await waitFor(() => {
      expect(useSessionStore.getState().activeSessionId).toBe("pty-existing");
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "pty_spawn",
      expect.anything(),
    );
    expect(onClose).toHaveBeenCalled();
  });

  test("shows actionable copy when an agent binary is missing", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_resolve_binary") {
        return null;
      }
      return null;
    });
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "pi",
      },
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText(/pi \(missing\)/i)).toBeTruthy();
    });
    fireEvent.click(screen.getByText("Start"));

    expect(
      await screen.findByText(
        "Pi is not on PATH. Install it, or update its launch command in Settings > Agents.",
      ),
    ).toBeTruthy();
  });

  test("runs Hermes through ACP without creating a fake PTY session", async () => {
    const onClose = vi.fn();
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args?: { command?: string }) => {
        if (command === "fs_resolve_binary" && args?.command === "hermes") {
          return "/usr/local/bin/hermes";
        }
        return null;
      },
    );
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "hermes",
      },
    });

    render(<NewSessionDialog open onClose={onClose} />);
    await waitFor(() => {
      expect(screen.getByText(/ACP command:\s*hermes acp/)).toBeTruthy();
      expect(screen.getByText(/usr\/local\/bin\/hermes/)).toBeTruthy();
    });
    fireEvent.change(screen.getByTestId("Initial prompt-plain-text"), {
      target: { value: "continue the task" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/adapters/hermes/acp/prompt",
        expect.objectContaining({ method: "POST" }),
      );
    });
    const [, request] = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/v1/adapters/hermes/acp/prompt"),
    )!;
    expect(JSON.parse(request.body as string)).toMatchObject({
      protocol_version: "1.0",
      cwd: "/repo",
      prompt: "continue the task",
      resumeSessionId: null,
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "pty_spawn",
      expect.anything(),
    );
    expect(useSessionStore.getState().activeSessionId).toBeNull();
    expect(await screen.findByText("ACP session hermes-session-1 (end_turn)")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  test("passes selected ACP resume session id", async () => {
    const onClose = vi.fn();
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args?: { command?: string }) => {
        if (command === "fs_resolve_binary" && args?.command === "hermes") {
          return "/usr/local/bin/hermes";
        }
        return null;
      },
    );
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "hermes",
      },
      sessions: [
        {
          id: "pty-hermes",
          agent: "hermes",
          workspaceId: "workspace:/repo",
          title: "Hermes · repo",
          status: "stale",
          createdAt: 1,
          pendingApprovals: [],
          cwd: "/repo",
          lastExitCode: null,
          lastTrigger: null,
          lastCommandBlockId: null,
          transcript: null,
          restart: null,
          remote: null,
          provider: {
            agent: "hermes",
            providerSessionId: "hermes-provider-9",
            conversationId: null,
            resume: {
              command: "hermes",
              args: ["acp", "resume", "fallback-session"],
              source: "native",
            },
            updatedAt: 1,
          },
        },
      ],
    });

    render(<NewSessionDialog open onClose={onClose} />);
    await waitFor(() => {
      expect(screen.getByText(/ACP command:\s*hermes acp/)).toBeTruthy();
      expect(screen.getByText(/usr\/local\/bin\/hermes/)).toBeTruthy();
    });
    const resumeSelect = await screen.findByLabelText("Resume ACP session");
    expect(screen.getByText("Hermes · repo - native: hermes-provider-9")).toBeTruthy();
    fireEvent.change(resumeSelect, {
      target: { value: "hermes-provider-9" },
    });
    fireEvent.change(screen.getByTestId("Initial prompt-plain-text"), {
      target: { value: "continue resumed task" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/adapters/hermes/acp/prompt",
        expect.objectContaining({ method: "POST" }),
      );
    });
    const [, request] = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/v1/adapters/hermes/acp/prompt"),
    )!;
    expect(JSON.parse(request.body as string)).toMatchObject({
      protocol_version: "1.0",
      cwd: "/repo",
      prompt: "continue resumed task",
      resumeSessionId: "hermes-provider-9",
    });
    expect(await screen.findByText("ACP session hermes-session-1 (end_turn)")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  test("requires a prompt before ACP launch", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args?: { command?: string }) => {
        if (command === "fs_resolve_binary" && args?.command === "hermes") {
          return "/usr/local/bin/hermes";
        }
        return null;
      },
    );
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "hermes",
      },
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText(/ACP command:\s*hermes acp/)).toBeTruthy();
      expect(screen.getByText(/usr\/local\/bin\/hermes/)).toBeTruthy();
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    expect(await screen.findByText("Enter an initial prompt for ACP launch.")).toBeTruthy();
    expect(
      fetchMock.mock.calls.some(([url]) =>
        String(url).includes("/v1/adapters/hermes/acp/prompt"),
      ),
    ).toBe(false);
  });

  test("runs Claude through ACP when config enables ACP transport", async () => {
    const onClose = vi.fn();
    fetchMock.mockImplementation(async (url: string) => {
      if (url.includes("/v1/config/status")) {
        return new Response(
          JSON.stringify({
            adapters: {
              claude: {
                transport: "acp",
                acpCommand: "claude-agent-acp",
                acpArgs: [],
              },
              hermes: { transport: "acp", acpCommand: "hermes", acpArgs: ["acp"] },
            },
            policyValidation: { ok: true },
          }),
          { status: 200 },
        );
      }
      if (url.includes("/v1/adapters/claude-code/acp/prompt")) {
        return new Response(
          JSON.stringify({
            protocol_version: "1.0",
            sessionId: "claude-acp-session-1",
            stopReason: "end_turn",
          }),
          { status: 200 },
        );
      }
      return new Response("not found", { status: 404 });
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args?: { command?: string }) => {
        if (command === "fs_resolve_binary" && args?.command === "claude-agent-acp") {
          return "/usr/local/bin/claude-agent-acp";
        }
        return null;
      },
    );
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "claude-code",
      },
    });

    render(<NewSessionDialog open onClose={onClose} />);
    await waitFor(() => {
      expect(screen.getByText(/ACP command:\s*claude-agent-acp/)).toBeTruthy();
      expect(screen.getByText(/usr\/local\/bin\/claude-agent-acp/)).toBeTruthy();
    });
    expect((screen.getByLabelText("Trust mode") as HTMLSelectElement).disabled).toBe(
      true,
    );
    expect((screen.getByLabelText("Auto worktree") as HTMLInputElement).disabled).toBe(
      true,
    );
    fireEvent.change(screen.getByTestId("Initial prompt-plain-text"), {
      target: { value: "review this branch" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/adapters/claude-code/acp/prompt",
        expect.objectContaining({ method: "POST" }),
      );
    });
    const [, request] = fetchMock.mock.calls.find(([url]) =>
      String(url).includes("/v1/adapters/claude-code/acp/prompt"),
    )!;
    expect(JSON.parse(request.body as string)).toMatchObject({
      protocol_version: "1.0",
      cwd: "/repo",
      prompt: "review this branch",
      resumeSessionId: null,
    });
    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "pty_spawn",
      expect.anything(),
    );
    expect(useSessionStore.getState().activeSessionId).toBeNull();
    expect(await screen.findByText("ACP session claude-acp-session-1 (end_turn)")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });

  test("surfaces ACP backend error bodies in the dialog", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url.includes("/v1/config/status")) {
        return new Response(
          JSON.stringify({
            adapters: {
              claude: { transport: "hook", acpCommand: "claude-agent-acp", acpArgs: [] },
              hermes: { transport: "acp", acpCommand: "hermes", acpArgs: ["acp"] },
            },
            policyValidation: { ok: true },
          }),
          { status: 200 },
        );
      }
      if (url.includes("/v1/adapters/hermes/acp/prompt")) {
        return new Response(JSON.stringify({ error: "ACP bridge unavailable" }), {
          status: 502,
        });
      }
      return new Response("not found", { status: 404 });
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args?: { command?: string }) => {
        if (command === "fs_resolve_binary" && args?.command === "hermes") {
          return "/usr/local/bin/hermes";
        }
        return null;
      },
    );
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        defaultAgent: "hermes",
      },
    });

    render(<NewSessionDialog open onClose={vi.fn()} />);
    await waitFor(() => {
      expect(screen.getByText(/ACP command:\s*hermes acp/)).toBeTruthy();
      expect(screen.getByText(/usr\/local\/bin\/hermes/)).toBeTruthy();
    });
    fireEvent.change(screen.getByTestId("Initial prompt-plain-text"), {
      target: { value: "continue the task" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Start" }));

    expect(
      await screen.findByText("hermes ACP prompt failed: ACP bridge unavailable"),
    ).toBeTruthy();
  });
});
