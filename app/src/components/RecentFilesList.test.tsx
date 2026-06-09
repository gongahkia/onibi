import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { RecentFilesList } from "./RecentFilesList";
import {
  DEFAULT_SETTINGS,
  bufferKey,
  useSessionStore,
  type MainSelection,
} from "../lib/sessions";

const fileAlpha: MainSelection = {
  type: "file",
  workspaceId: "workspace:/repo",
  workspaceRoot: "/repo",
  path: "/repo/alpha.ts",
  name: "alpha.ts",
  size: 1,
};
const fileBeta: MainSelection = {
  type: "file",
  workspaceId: "workspace:/repo",
  workspaceRoot: "/repo",
  path: "/repo/beta.ts",
  name: "beta.ts",
  size: 1,
};
const fileGamma: MainSelection = {
  type: "file",
  workspaceId: "workspace:/repo",
  workspaceRoot: "/repo",
  path: "/repo/gamma.ts",
  name: "gamma.ts",
  size: 1,
};
const gitDiff: MainSelection = {
  type: "git-diff",
  workspaceId: "workspace:/repo",
  workspaceRoot: "/repo",
  path: "src/main.ts",
  name: "main.ts",
  stage: "working",
};

function resetStore(patch: Partial<ReturnType<typeof useSessionStore.getState>> = {}) {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaces: [],
    workspaceTabs: [],
    selectedFile: null,
    openBuffers: [],
    closedBufferStack: [],
    bufferAccessOrder: [],
    activeBufferKey: null,
    settings: DEFAULT_SETTINGS,
    ...patch,
  });
}

describe("RecentFilesList", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders empty-state copy when there is no recent activity", () => {
    render(<RecentFilesList />);
    expect(screen.getByText("No recent files")).toBeTruthy();
  });

  test("orders open buffers by most-recently-used first", () => {
    resetStore({
      openBuffers: [fileAlpha, fileBeta, fileGamma],
      bufferAccessOrder: [
        bufferKey(fileAlpha),
        bufferKey(fileGamma),
        bufferKey(fileBeta),
      ],
    });
    render(<RecentFilesList />);
    const items = screen.getAllByRole("button");
    expect(items[0].textContent).toContain("beta.ts");
    expect(items[1].textContent).toContain("gamma.ts");
    expect(items[2].textContent).toContain("alpha.ts");
  });

  test("appends closed buffers after open ones and dedupes", () => {
    resetStore({
      openBuffers: [fileAlpha],
      closedBufferStack: [fileBeta, fileAlpha, fileGamma],
      bufferAccessOrder: [bufferKey(fileAlpha)],
    });
    render(<RecentFilesList />);
    const items = screen.getAllByRole("button");
    expect(items.map((item) => item.textContent)).toHaveLength(3);
    expect(items[0].textContent).toContain("alpha.ts");
    expect(items[1].textContent).toContain("beta.ts");
    expect(items[2].textContent).toContain("gamma.ts");
  });

  test("clicking a recent item calls selectFile", () => {
    resetStore({
      openBuffers: [fileAlpha, fileBeta],
      bufferAccessOrder: [bufferKey(fileAlpha), bufferKey(fileBeta)],
    });
    render(<RecentFilesList />);
    fireEvent.click(screen.getByText("alpha.ts"));
    expect(useSessionStore.getState().selectedFile).toEqual(fileAlpha);
  });

  test("marks the active buffer", () => {
    resetStore({
      openBuffers: [fileAlpha, fileBeta],
      bufferAccessOrder: [bufferKey(fileAlpha), bufferKey(fileBeta)],
      activeBufferKey: bufferKey(fileAlpha),
    });
    render(<RecentFilesList />);
    const items = screen.getAllByRole("button");
    const active = items.find((item) => item.className.includes("active"));
    expect(active?.textContent).toContain("alpha.ts");
  });

  test("marks open buffers distinct from closed", () => {
    resetStore({
      openBuffers: [fileAlpha],
      closedBufferStack: [fileBeta],
      bufferAccessOrder: [bufferKey(fileAlpha)],
    });
    render(<RecentFilesList />);
    const items = screen.getAllByRole("button");
    expect(items[0].className).toContain("open");
    expect(items[1].className).not.toContain("open");
  });

  test("describes git-diff buffers with stage and path", () => {
    resetStore({
      openBuffers: [gitDiff],
      bufferAccessOrder: [bufferKey(gitDiff)],
    });
    render(<RecentFilesList />);
    expect(screen.getByText("main.ts")).toBeTruthy();
    expect(screen.getByText("git · working · src/main.ts")).toBeTruthy();
  });
});
