import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { EditorTabBar } from "./EditorTabBar";
import { bufferKey, useSessionStore, type MainSelection } from "../lib/sessions";

function fileSelection(path: string): MainSelection {
  return {
    type: "file",
    workspaceId: "workspace:/repo",
    workspaceRoot: "/repo",
    path,
    name: path.split("/").pop() ?? path,
    size: 0,
  };
}

function reset() {
  useSessionStore.setState({
    openBuffers: [],
    activeBufferKey: null,
    selectedFile: null,
    sessionEvents: [],
  });
}

describe("EditorTabBar", () => {
  beforeEach(() => {
    reset();
  });

  test("renders nothing when there are no open buffers", () => {
    const { container } = render(<EditorTabBar />);
    expect(container.firstChild).toBeNull();
  });

  test("renders a tab per open buffer with the active one marked", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    useSessionStore.setState({
      openBuffers: [a, b],
      activeBufferKey: bufferKey(b),
      selectedFile: b,
    });
    render(<EditorTabBar />);
    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(2);
    expect(tabs[0].getAttribute("aria-selected")).toBe("false");
    expect(tabs[1].getAttribute("aria-selected")).toBe("true");
  });

  test("clicking a tab makes it active without removing other tabs", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    useSessionStore.setState({
      openBuffers: [a, b],
      activeBufferKey: bufferKey(b),
      selectedFile: b,
    });
    render(<EditorTabBar />);
    fireEvent.click(screen.getAllByRole("tab")[0]);
    const state = useSessionStore.getState();
    expect(state.activeBufferKey).toBe(bufferKey(a));
    expect(state.openBuffers).toHaveLength(2);
    expect(state.selectedFile?.path).toBe("/repo/a.ts");
  });

  test("close on the active tab focuses the next buffer", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    const c = fileSelection("/repo/c.ts");
    useSessionStore.setState({
      openBuffers: [a, b, c],
      activeBufferKey: bufferKey(b),
      selectedFile: b,
    });
    render(<EditorTabBar />);
    fireEvent.click(screen.getByLabelText("Close b.ts"));
    const state = useSessionStore.getState();
    expect(state.openBuffers.map((buffer) => buffer.path)).toEqual([
      "/repo/a.ts",
      "/repo/c.ts",
    ]);
    expect(state.activeBufferKey).toBe(bufferKey(c));
  });

  test("closing the last tab clears the editor surface", () => {
    const a = fileSelection("/repo/a.ts");
    useSessionStore.setState({
      openBuffers: [a],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    render(<EditorTabBar />);
    fireEvent.click(screen.getByLabelText("Close a.ts"));
    const state = useSessionStore.getState();
    expect(state.openBuffers).toEqual([]);
    expect(state.activeBufferKey).toBeNull();
    expect(state.selectedFile).toBeNull();
  });

  test("selectFile focuses an existing buffer instead of duplicating", () => {
    const a = fileSelection("/repo/a.ts");
    useSessionStore.setState({
      openBuffers: [a],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    useSessionStore.getState().selectFile(fileSelection("/repo/a.ts"));
    const state = useSessionStore.getState();
    expect(state.openBuffers).toHaveLength(1);
    expect(state.activeBufferKey).toBe(bufferKey(a));
  });

  test("selectFile appends a new buffer and makes it active", () => {
    const a = fileSelection("/repo/a.ts");
    useSessionStore.setState({
      openBuffers: [a],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    useSessionStore.getState().selectFile(fileSelection("/repo/b.ts"));
    const state = useSessionStore.getState();
    expect(state.openBuffers.map((buffer) => buffer.path)).toEqual([
      "/repo/a.ts",
      "/repo/b.ts",
    ]);
    expect(state.selectedFile?.path).toBe("/repo/b.ts");
  });

  test("selectFile(null) hides the editor but keeps tabs", () => {
    const a = fileSelection("/repo/a.ts");
    useSessionStore.setState({
      openBuffers: [a],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    useSessionStore.getState().selectFile(null);
    const state = useSessionStore.getState();
    expect(state.openBuffers).toHaveLength(1);
    expect(state.activeBufferKey).toBeNull();
    expect(state.selectedFile).toBeNull();
  });

  test("removeWorkspace prunes buffers from that workspace", () => {
    const repoFile = fileSelection("/repo/a.ts");
    const otherFile: MainSelection = {
      type: "file",
      workspaceId: "workspace:/other",
      workspaceRoot: "/other",
      path: "/other/x.ts",
      name: "x.ts",
      size: 0,
    };
    useSessionStore.setState({
      openBuffers: [repoFile, otherFile],
      activeBufferKey: bufferKey(repoFile),
      selectedFile: repoFile,
      workspaces: [
        { id: "workspace:/repo", path: "/repo", name: "repo" },
        { id: "workspace:/other", path: "/other", name: "other" },
      ],
    });
    useSessionStore.getState().removeWorkspace("workspace:/repo");
    const state = useSessionStore.getState();
    expect(state.openBuffers).toHaveLength(1);
    expect(state.openBuffers[0].workspaceId).toBe("workspace:/other");
    expect(state.activeBufferKey).toBeNull();
    expect(state.selectedFile).toBeNull();
  });
});
