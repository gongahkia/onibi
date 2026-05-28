import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { CommandPalette } from "./CommandPalette";
import { DEFAULT_SETTINGS, type Session, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue([]);
}

describe("CommandPalette", () => {
  beforeEach(() => {
    resetStore();
    vi.clearAllMocks();
  });

  test("opens with ctrl+p and executes a settings command", () => {
    render(<CommandPalette />);

    fireEvent.keyDown(window, { key: "p", ctrlKey: true });
    const input = screen.getByLabelText("Search commands");
    fireEvent.change(input, { target: { value: "theme light" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(useSessionStore.getState().settings.theme).toBe("light");
    expect(screen.queryByRole("dialog", { name: "Command palette" })).toBeNull();
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
});
