import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { NewSessionDialog } from "./NewSessionDialog";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    selectedFile: null,
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
});
