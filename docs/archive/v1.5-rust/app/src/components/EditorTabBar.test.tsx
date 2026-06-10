import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { EditorTabBar } from "./EditorTabBar";
import {
  DEFAULT_SETTINGS,
  bufferKey,
  useSessionStore,
  type MainSelection,
} from "../lib/sessions";

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
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
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

  test("closing a tab pushes it to the closedBufferStack and reopen restores it", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    useSessionStore.setState({
      openBuffers: [a, b],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
      closedBufferStack: [],
    });
    useSessionStore.getState().closeBuffer(bufferKey(a));
    let state = useSessionStore.getState();
    expect(state.openBuffers.map((buf) => buf.path)).toEqual(["/repo/b.ts"]);
    expect(state.closedBufferStack.map((buf) => buf.path)).toEqual(["/repo/a.ts"]);

    const reopened = useSessionStore.getState().reopenClosedBuffer();
    state = useSessionStore.getState();
    expect(reopened).toBe(true);
    expect(state.openBuffers.map((buf) => buf.path)).toEqual([
      "/repo/b.ts",
      "/repo/a.ts",
    ]);
    expect(state.activeBufferKey).toBe(bufferKey(a));
    expect(state.closedBufferStack).toEqual([]);
  });

  test("reopenClosedBuffer returns false when the stack is empty", () => {
    useSessionStore.setState({
      openBuffers: [],
      activeBufferKey: null,
      selectedFile: null,
      closedBufferStack: [],
    });
    expect(useSessionStore.getState().reopenClosedBuffer()).toBe(false);
  });

  test("closedBufferStack is bounded at 20 entries", () => {
    const buffers = Array.from({ length: 25 }, (_, i) =>
      fileSelection(`/repo/${i}.ts`),
    );
    useSessionStore.setState({
      openBuffers: buffers,
      activeBufferKey: bufferKey(buffers[0]),
      selectedFile: buffers[0],
      closedBufferStack: [],
    });
    useSessionStore.getState().closeAllBuffers();
    const state = useSessionStore.getState();
    expect(state.openBuffers).toHaveLength(0);
    expect(state.closedBufferStack).toHaveLength(20);
    // Stack is most-recently-closed-first; closeAllBuffers pushes the latest
    // open buffer onto the stack first (reversed). The 25th open is /24.ts.
    expect(state.closedBufferStack[0].path).toBe("/repo/24.ts");
    // And the oldest kept on the stack is the 5th-most-recent (idx 5).
    expect(state.closedBufferStack[19].path).toBe("/repo/5.ts");
  });

  test("setBufferDirty toggles dirty state and the tab class follows", () => {
    const a = fileSelection("/repo/a.ts");
    useSessionStore.setState({
      openBuffers: [a],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
      dirtyBufferKeys: [],
    });
    render(<EditorTabBar />);
    act(() => {
      useSessionStore.getState().setBufferDirty(bufferKey(a), true);
    });
    const tab = screen.getAllByRole("tab")[0];
    expect(tab.className).toContain("dirty");
    act(() => {
      useSessionStore.getState().setBufferDirty(bufferKey(a), false);
    });
    expect(tab.className).not.toContain("dirty");
  });

  test("reorderBuffer moves a tab before another", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    const c = fileSelection("/repo/c.ts");
    useSessionStore.setState({
      openBuffers: [a, b, c],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    // Move 'a' to after 'c'
    useSessionStore.getState().reorderBuffer(bufferKey(a), bufferKey(c), false);
    const state = useSessionStore.getState();
    expect(state.openBuffers.map((buf) => buf.path)).toEqual([
      "/repo/b.ts",
      "/repo/c.ts",
      "/repo/a.ts",
    ]);
  });

  test("reorderBuffer can move a later tab before an earlier one", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    const c = fileSelection("/repo/c.ts");
    useSessionStore.setState({
      openBuffers: [a, b, c],
      activeBufferKey: bufferKey(a),
      selectedFile: a,
    });
    // Move 'c' to before 'a'
    useSessionStore.getState().reorderBuffer(bufferKey(c), bufferKey(a), true);
    const state = useSessionStore.getState();
    expect(state.openBuffers.map((buf) => buf.path)).toEqual([
      "/repo/c.ts",
      "/repo/a.ts",
      "/repo/b.ts",
    ]);
  });

  test("editorOpenLimit evicts the LRU non-active tab on selectFile", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    const c = fileSelection("/repo/c.ts");
    useSessionStore.setState({
      openBuffers: [a, b, c],
      activeBufferKey: bufferKey(c),
      selectedFile: c,
      bufferAccessOrder: [bufferKey(a), bufferKey(b), bufferKey(c)],
      closedBufferStack: [],
      dirtyBufferKeys: [],
      settings: {
        ...DEFAULT_SETTINGS,
        editorOpenLimit: 3,
      },
    });
    const d = fileSelection("/repo/d.ts");
    useSessionStore.getState().selectFile(d);
    const state = useSessionStore.getState();
    expect(state.openBuffers.map((buf) => buf.path)).toEqual([
      "/repo/b.ts",
      "/repo/c.ts",
      "/repo/d.ts",
    ]);
    expect(state.closedBufferStack[0].path).toBe("/repo/a.ts");
    expect(state.activeBufferKey).toBe(bufferKey(d));
  });

  test("editorOpenLimit never evicts the active or a dirty tab", () => {
    const a = fileSelection("/repo/a.ts");
    const b = fileSelection("/repo/b.ts");
    const c = fileSelection("/repo/c.ts");
    useSessionStore.setState({
      openBuffers: [a, b, c],
      activeBufferKey: bufferKey(c),
      selectedFile: c,
      bufferAccessOrder: [bufferKey(a), bufferKey(b), bufferKey(c)],
      closedBufferStack: [],
      // 'a' is dirty even though it's the LRU
      dirtyBufferKeys: [bufferKey(a)],
      settings: {
        ...DEFAULT_SETTINGS,
        editorOpenLimit: 3,
      },
    });
    const d = fileSelection("/repo/d.ts");
    useSessionStore.getState().selectFile(d);
    const state = useSessionStore.getState();
    // 'a' kept (dirty), 'b' evicted (next-LRU, non-dirty, non-active)
    expect(state.openBuffers.map((buf) => buf.path)).toEqual([
      "/repo/a.ts",
      "/repo/c.ts",
      "/repo/d.ts",
    ]);
    expect(state.closedBufferStack[0].path).toBe("/repo/b.ts");
  });

  test("editorOpenLimit = 0 means unlimited", () => {
    useSessionStore.setState({
      openBuffers: [],
      activeBufferKey: null,
      selectedFile: null,
      bufferAccessOrder: [],
      closedBufferStack: [],
      dirtyBufferKeys: [],
      settings: {
        ...DEFAULT_SETTINGS,
        editorOpenLimit: 0,
      },
    });
    for (let i = 0; i < 15; i++) {
      useSessionStore.getState().selectFile(fileSelection(`/repo/${i}.ts`));
    }
    expect(useSessionStore.getState().openBuffers).toHaveLength(15);
  });

  test("lowering editorOpenLimit immediately evicts buffers", () => {
    const buffers = Array.from({ length: 8 }, (_, i) =>
      fileSelection(`/repo/${i}.ts`),
    );
    useSessionStore.setState({
      openBuffers: buffers,
      activeBufferKey: bufferKey(buffers[7]),
      selectedFile: buffers[7],
      bufferAccessOrder: buffers.map((b) => bufferKey(b)),
      closedBufferStack: [],
      dirtyBufferKeys: [],
      settings: { ...DEFAULT_SETTINGS, editorOpenLimit: 10 },
    });
    useSessionStore.getState().updateSettings({ editorOpenLimit: 3 });
    const state = useSessionStore.getState();
    expect(state.openBuffers).toHaveLength(3);
    expect(state.openBuffers[state.openBuffers.length - 1].path).toBe("/repo/7.ts");
  });

  test("closedBufferHistoryLimit caps the reopen stack", () => {
    const buffers = Array.from({ length: 12 }, (_, i) =>
      fileSelection(`/repo/${i}.ts`),
    );
    useSessionStore.setState({
      openBuffers: buffers,
      activeBufferKey: bufferKey(buffers[0]),
      selectedFile: buffers[0],
      bufferAccessOrder: buffers.map((b) => bufferKey(b)),
      closedBufferStack: [],
      dirtyBufferKeys: [],
      settings: { ...DEFAULT_SETTINGS, closedBufferHistoryLimit: 5 },
    });
    useSessionStore.getState().closeAllBuffers();
    expect(useSessionStore.getState().closedBufferStack).toHaveLength(5);
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
