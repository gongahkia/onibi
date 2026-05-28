import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { FileTree } from "./FileTree";
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

describe("FileTree", () => {
  beforeEach(() => {
    resetStore();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("expands and selects a lazy-loaded file", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([
      { name: "src", path: "/repo/src", kind: "dir", size: 0 },
      { name: "README.md", path: "/repo/README.md", kind: "file", size: 12 },
    ]);

    render(<FileTree />);
    fireEvent.doubleClick(screen.getByText("repo"));

    expect(await screen.findByText("README.md")).toBeTruthy();
    fireEvent.click(screen.getByText("README.md"));

    expect(useSessionStore.getState().selectedFile?.path).toBe("/repo/README.md");
  });

  test("shows sandbox errors returned by fs commands", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockRejectedValueOnce(
      "path /etc escapes workspace /repo",
    );

    render(<FileTree />);
    fireEvent.doubleClick(screen.getByText("repo"));

    await waitFor(() => {
      expect(screen.getByText(/escapes workspace/)).toBeTruthy();
    });
  });

  test("opens a native folder picker when adding a workspace", async () => {
    globalThis.__TAURI_MOCKS__.dialogOpen.mockResolvedValue("/repo/new");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_workspace_info") {
        return { path: "/repo/new", name: "new" };
      }
      if (command === "fs_list_dir") {
        return [];
      }
      return null;
    });

    render(<FileTree />);
    fireEvent.click(screen.getByLabelText("Open Folder"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.dialogOpen).toHaveBeenCalledWith({
        directory: true,
        multiple: false,
        title: "Choose workspace folder",
      });
    });
    expect(await screen.findByText("new")).toBeTruthy();
    expect(useSessionStore.getState().workspaces).toContainEqual({
      id: "workspace:/repo/new",
      path: "/repo/new",
      name: "new",
    });
  });
});
