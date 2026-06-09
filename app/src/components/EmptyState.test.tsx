import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { EmptyState } from "./EmptyState";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
}

describe("EmptyState", () => {
  beforeEach(() => {
    resetStore();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => [],
      })),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  test("renders the start prompt without landing-page help copy", () => {
    render(<EmptyState />);
    expect(screen.getByText("Start")).toBeTruthy();
    expect(screen.queryByText("Help")).toBeNull();
    expect(screen.queryByText("Local AI agent cockpit. Open a folder, start a session.")).toBeNull();
    expect(screen.queryByText("Clone Git Repository...")).toBeNull();
  });
});
