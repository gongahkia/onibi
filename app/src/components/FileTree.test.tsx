import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { FileTree } from "./FileTree";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [
      {
        id: "session-1",
        agent: "opencode",
        workspaceId: "workspace:/repo",
        title: "OpenCode repo",
        status: "running",
        createdAt: 1,
        pendingApprovals: [],
      },
    ],
    activeSessionId: "session-1",
    workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.dialogOpen.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.openerRevealItemInDir.mockReset();
  vi.mocked(navigator.clipboard.writeText).mockClear();
  vi.mocked(window.prompt).mockReturnValue(null);
}

function createDataTransfer(): DataTransfer {
  const data = new Map<string, string>();
  return {
    dropEffect: "none",
    effectAllowed: "all",
    files: [] as unknown as FileList,
    items: [] as unknown as DataTransferItemList,
    types: [],
    clearData: vi.fn((type?: string) => {
      if (type) {
        data.delete(type);
      } else {
        data.clear();
      }
    }),
    getData: vi.fn((type: string) => data.get(type) ?? ""),
    setData: vi.fn((type: string, value: string) => {
      data.set(type, value);
    }),
    setDragImage: vi.fn(),
  } as unknown as DataTransfer;
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

    expect(await screen.findByText("README.md")).toBeTruthy();
    fireEvent.click(screen.getByText("README.md"));

    expect(useSessionStore.getState().selectedFile?.path).toBe("/repo/README.md");
    expect(screen.getByTitle("Markdown file")).toBeTruthy();
  });

  test("hides file icons when disabled in settings", async () => {
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, showFileIcons: false },
    });
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([
      { name: "README.md", path: "/repo/README.md", kind: "file", size: 12 },
    ]);

    render(<FileTree />);

    expect(await screen.findByText("README.md")).toBeTruthy();
    expect(screen.queryByTitle("Markdown file")).toBeNull();
  });

  test("scopes the visible tree to the active session workspace", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "session-1",
          agent: "opencode",
          workspaceId: "workspace:/repo/kelp-claw",
          title: "OpenCode kelp-claw",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "session-1",
      workspaces: [
        {
          id: "workspace:/repo/bob_frontend",
          path: "/repo/bob_frontend",
          name: "bob_frontend",
        },
        {
          id: "workspace:/repo/kelp-claw",
          path: "/repo/kelp-claw",
          name: "kelp-claw",
        },
      ],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([
      {
        name: "package.json",
        path: "/repo/kelp-claw/package.json",
        kind: "file",
        size: 24,
      },
    ]);

    render(<FileTree />);

    expect(screen.queryByText("bob_frontend")).toBeNull();
    expect(screen.getByText("kelp-claw")).toBeTruthy();
    expect(await screen.findByText("package.json")).toBeTruthy();
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("fs_list_dir", {
      root: "/repo/kelp-claw",
      path: "/repo/kelp-claw",
    });
  });

  test("does not show every saved workspace when no session is active", () => {
    useSessionStore.setState({
      sessions: [],
      activeSessionId: null,
      workspaces: [
        {
          id: "workspace:/repo/bob_frontend",
          path: "/repo/bob_frontend",
          name: "bob_frontend",
        },
        {
          id: "workspace:/repo/kelp-claw",
          path: "/repo/kelp-claw",
          name: "kelp-claw",
        },
      ],
      selectedFile: {
        workspaceId: "workspace:/repo/bob_frontend",
        workspaceRoot: "/repo/bob_frontend",
        path: "/repo/bob_frontend/package.json",
        name: "package.json",
        size: 24,
      },
    });

    render(<FileTree />);

    expect(screen.queryByText("bob_frontend")).toBeNull();
    expect(screen.queryByText("kelp-claw")).toBeNull();
    expect(screen.getByLabelText("New file").hasAttribute("disabled")).toBe(true);
    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "fs_list_dir",
      expect.anything(),
    );
  });

  test("switches the tree when the active session changes workspaces", async () => {
    useSessionStore.setState({
      sessions: [
        {
          id: "session-bob",
          agent: "claude-code",
          workspaceId: "workspace:/repo/bob_frontend",
          title: "Claude Code bob_frontend",
          status: "running",
          createdAt: 1,
          pendingApprovals: [],
        },
        {
          id: "session-kelp",
          agent: "opencode",
          workspaceId: "workspace:/repo/kelp-claw",
          title: "OpenCode kelp-claw",
          status: "running",
          createdAt: 2,
          pendingApprovals: [],
        },
      ],
      activeSessionId: "session-bob",
      workspaces: [
        {
          id: "workspace:/repo/bob_frontend",
          path: "/repo/bob_frontend",
          name: "bob_frontend",
        },
        {
          id: "workspace:/repo/kelp-claw",
          path: "/repo/kelp-claw",
          name: "kelp-claw",
        },
      ],
    });
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (_command: string, args: Record<string, string>) => {
        if (args.root === "/repo/bob_frontend") {
          return [
            {
              name: "vite.config.js",
              path: "/repo/bob_frontend/vite.config.js",
              kind: "file",
              size: 12,
            },
          ];
        }
        if (args.root === "/repo/kelp-claw") {
          return [
            {
              name: "pnpm-workspace.yaml",
              path: "/repo/kelp-claw/pnpm-workspace.yaml",
              kind: "file",
              size: 12,
            },
          ];
        }
        return [];
      },
    );

    render(<FileTree />);

    expect(screen.getByText("bob_frontend")).toBeTruthy();
    expect(screen.queryByText("kelp-claw")).toBeNull();
    expect(await screen.findByText("vite.config.js")).toBeTruthy();

    useSessionStore.getState().setActiveSession("session-kelp");

    await waitFor(() => {
      expect(screen.getByText("kelp-claw")).toBeTruthy();
    });
    expect(screen.queryByText("bob_frontend")).toBeNull();
    expect(await screen.findByText("pnpm-workspace.yaml")).toBeTruthy();
  });

  test("shows sandbox errors returned by fs commands", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockRejectedValueOnce(
      "path /etc escapes workspace /repo",
    );

    render(<FileTree />);

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
    expect(useSessionStore.getState().workspaces).toContainEqual({
      id: "workspace:/repo/new",
      path: "/repo/new",
      name: "new",
    });
  });

  test("creates a file from the tree toolbar", async () => {
    vi.mocked(window.prompt).mockReturnValue("note.txt");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, string>) => {
        if (command === "fs_create_file") {
          expect(args).toEqual({
            root: "/repo",
            parent: "/repo",
            name: "note.txt",
          });
          return { name: "note.txt", path: "/repo/note.txt", kind: "file", size: 0 };
        }
        if (command === "fs_list_dir") {
          return [{ name: "note.txt", path: "/repo/note.txt", kind: "file", size: 0 }];
        }
        return null;
      },
    );

    render(<FileTree />);
    fireEvent.click(screen.getByLabelText("New file"));

    expect(await screen.findByText("note.txt")).toBeTruthy();
    expect(useSessionStore.getState().selectedFile?.path).toBe("/repo/note.txt");
  });

  test("shows supported context actions for files", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([
      { name: "README.md", path: "/repo/README.md", kind: "file", size: 12 },
    ]);

    render(<FileTree />);
    expect(await screen.findByText("README.md")).toBeTruthy();

    fireEvent.contextMenu(screen.getByText("README.md"));

    expect(screen.getByRole("menu")).toBeTruthy();
    fireEvent.click(screen.getByText("Copy Relative Path"));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith("README.md");
  });

  test("renames files from the context menu and refreshes the parent", async () => {
    vi.mocked(window.prompt).mockReturnValue("CHANGELOG.md");
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, string>) => {
        if (command === "fs_list_dir") {
          return [{ name: "README.md", path: "/repo/README.md", kind: "file", size: 12 }];
        }
        if (command === "fs_rename_path") {
          expect(args).toEqual({
            root: "/repo",
            path: "/repo/README.md",
            name: "CHANGELOG.md",
          });
          return {
            name: "CHANGELOG.md",
            path: "/repo/CHANGELOG.md",
            kind: "file",
            size: 12,
          };
        }
        return null;
      },
    );

    render(<FileTree />);
    expect(await screen.findByText("README.md")).toBeTruthy();

    fireEvent.contextMenu(screen.getByText("README.md"));
    fireEvent.click(screen.getByText("Rename..."));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "fs_rename_path",
        {
          root: "/repo",
          path: "/repo/README.md",
          name: "CHANGELOG.md",
        },
      );
    });
  });

  test("reveals files in Finder from the context menu", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([
      { name: "README.md", path: "/repo/README.md", kind: "file", size: 12 },
    ]);

    render(<FileTree />);
    expect(await screen.findByText("README.md")).toBeTruthy();

    fireEvent.contextMenu(screen.getByText("README.md"));
    fireEvent.click(screen.getByText("Reveal in Finder"));

    expect(globalThis.__TAURI_MOCKS__.openerRevealItemInDir).toHaveBeenCalledWith(
      "/repo/README.md",
    );
  });

  test("moves files by dragging them onto folders", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, string>) => {
        if (command === "fs_list_dir" && args.path === "/repo") {
          return [
            { name: "src", path: "/repo/src", kind: "dir", size: 0 },
            { name: "README.md", path: "/repo/README.md", kind: "file", size: 12 },
          ];
        }
        if (command === "fs_move_path") {
          expect(args).toEqual({
            root: "/repo",
            path: "/repo/README.md",
            destination: "/repo/src",
          });
          return {
            name: "README.md",
            path: "/repo/src/README.md",
            kind: "file",
            size: 12,
          };
        }
        if (command === "fs_list_dir" && args.path === "/repo/src") {
          return [
            {
              name: "README.md",
              path: "/repo/src/README.md",
              kind: "file",
              size: 12,
            },
          ];
        }
        return [];
      },
    );
    const dataTransfer = createDataTransfer();

    render(<FileTree />);
    expect(await screen.findByText("README.md")).toBeTruthy();

    fireEvent.dragStart(screen.getByText("README.md"), { dataTransfer });
    fireEvent.dragOver(screen.getByText("src"), { dataTransfer });
    fireEvent.drop(screen.getByText("src"), { dataTransfer });

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "fs_move_path",
        {
          root: "/repo",
          path: "/repo/README.md",
          destination: "/repo/src",
        },
      );
    });
  });

  test("ignores dropping folders onto their descendants", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(
      async (command: string, args: Record<string, string>) => {
        if (command === "fs_list_dir" && args.path === "/repo") {
          return [{ name: "src", path: "/repo/src", kind: "dir", size: 0 }];
        }
        if (command === "fs_list_dir" && args.path === "/repo/src") {
          return [
            { name: "child", path: "/repo/src/child", kind: "dir", size: 0 },
          ];
        }
        return [];
      },
    );
    const dataTransfer = createDataTransfer();

    render(<FileTree />);
    expect(await screen.findByText("src")).toBeTruthy();
    fireEvent.click(screen.getByText("src"));
    expect(await screen.findByText("child")).toBeTruthy();

    fireEvent.dragStart(screen.getByText("src"), { dataTransfer });
    fireEvent.dragOver(screen.getByText("child"), { dataTransfer });
    fireEvent.drop(screen.getByText("child"), { dataTransfer });

    expect(globalThis.__TAURI_MOCKS__.invoke).not.toHaveBeenCalledWith(
      "fs_move_path",
      expect.anything(),
    );
  });
});
