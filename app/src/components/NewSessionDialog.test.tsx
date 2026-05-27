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
          env: [],
          rows: 30,
          cols: 100,
        },
      });
    });
    expect(useSessionStore.getState().activeSessionId).toBe("pty-shell");
  });
});
