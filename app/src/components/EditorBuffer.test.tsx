import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { EditorBuffer } from "./EditorBuffer";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

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
}

describe("EditorBuffer", () => {
  beforeEach(() => {
    resetStore();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("marks dirty and saves edited text", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([104, 105]);
    render(<EditorBuffer path="/repo/a.txt" workspaceRoot="/repo" />);

    const editor = await screen.findByLabelText("Editor buffer");
    expect(document.querySelector(".cm-lineNumbers")).toBeTruthy();
    const view = EditorView.findFromDOM(editor as HTMLElement);
    expect(view).toBeTruthy();
    act(() => {
      view?.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: "hello" },
      });
    });
    expect(screen.getByLabelText("dirty")).toBeTruthy();

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "fs_write_file",
        {
          root: "/repo",
          path: "/repo/a.txt",
          data: [104, 101, 108, 108, 111],
        },
      );
    });
  });

  test("refuses binary files", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([0, 1, 2]);
    render(<EditorBuffer path="/repo/blob.bin" workspaceRoot="/repo" />);

    expect(await screen.findByText("Cannot edit binary file.")).toBeTruthy();
  });
});
