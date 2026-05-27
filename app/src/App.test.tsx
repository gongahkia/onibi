import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import App from "./App";
import { DEFAULT_SETTINGS, useSessionStore } from "./lib/sessions";

vi.mock("./components/AgentTabBar", () => ({
  AgentTabBar: ({ orientation }: { orientation: string }) => (
    <div data-testid="agent-tab-bar">{orientation}</div>
  ),
}));

vi.mock("./components/FileTree", () => ({
  FileTree: () => <div data-testid="file-tree" />,
}));

vi.mock("./components/MainPane", () => ({
  MainPane: () => <div data-testid="main-pane" />,
}));

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

describe("App", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders horizontal tab layout when configured", () => {
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        tabBarOrientation: "horizontal",
        tabBarPosition: "top",
      },
    });

    render(<App />);

    expect(screen.getByTestId("agent-tab-bar").textContent).toContain("horizontal");
    expect(screen.getByRole("main").getAttribute("data-tab-orientation")).toBe(
      "horizontal",
    );
  });
});
