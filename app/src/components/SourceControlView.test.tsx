import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { SourceControlView } from "./SourceControlView";
import type { GitStatus } from "../lib/git";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

const workspace = { id: "workspace:/repo", path: "/repo", name: "repo" };

const status: GitStatus = {
  isRepo: true,
  repoRoot: "/repo",
  branch: "main",
  upstream: "origin/main",
  ahead: 1,
  behind: 2,
  entries: [
    {
      path: "README.md",
      fullPath: "/repo/README.md",
      indexStatus: null,
      worktreeStatus: "M",
    },
    {
      path: "src/index.ts",
      fullPath: "/repo/src/index.ts",
      indexStatus: "A",
      worktreeStatus: null,
    },
  ],
};

describe("SourceControlView", () => {
  beforeEach(() => {
    globalThis.__TAURI_MOCKS__.invoke.mockReset();
    vi.mocked(window.confirm).mockReturnValue(true);
    useSessionStore.setState({
      hydrated: true,
      sessions: [],
      activeSessionId: null,
      workspaces: [],
      selectedFile: null,
      settings: DEFAULT_SETTINGS,
    });
  });

  test("renders repo state and stages changed paths", async () => {
    const onRefresh = vi.fn(async () => undefined);
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue("");

    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={onRefresh}
      />,
    );

    expect(screen.getByText("main")).toBeTruthy();
    expect(screen.getByText("Sync 2↓ 1↑")).toBeTruthy();
    expect(screen.getByText("README.md")).toBeTruthy();

    fireEvent.click(screen.getByLabelText("Stage README.md"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "git_stage_paths",
        {
          root: "/repo",
          paths: ["README.md"],
        },
      );
    });
    expect(onRefresh).toHaveBeenCalled();
  });

  test("commits staged changes with a message", async () => {
    const onRefresh = vi.fn(async () => undefined);
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue("");

    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={onRefresh}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText("Message (main)"), {
      target: { value: "Add source view" },
    });
    fireEvent.click(screen.getByText("Commit"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith("git_commit", {
        root: "/repo",
        message: "Add source view",
      });
    });
  });

  test("opens clicked changed files as git diffs", () => {
    render(
      <SourceControlView
        workspace={workspace}
        status={status}
        loading={false}
        error=""
        onRefresh={vi.fn(async () => undefined)}
      />,
    );

    fireEvent.click(screen.getByText("README.md"));

    expect(useSessionStore.getState().selectedFile).toMatchObject({
      type: "git-diff",
      workspaceRoot: "/repo",
      path: "README.md",
      stage: "working",
    });
  });
});
