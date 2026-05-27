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
});
