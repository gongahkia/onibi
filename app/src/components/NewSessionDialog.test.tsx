import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { NewSessionDialog } from "./NewSessionDialog";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

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
}

describe("NewSessionDialog", () => {
  beforeEach(() => {
    resetStore();
  });

  afterEach(() => {
    vi.clearAllMocks();
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
          rows: 30,
          cols: 100,
          agent: "shell",
          workspaceId: "workspace:/repo",
          title: "Plain shell · repo",
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
    fireEvent.change(screen.getByLabelText("Initial prompt"), {
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
        },
      });
    });
    expect(useSessionStore.getState().activeSessionId).toBe("pty-codex");
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
});
