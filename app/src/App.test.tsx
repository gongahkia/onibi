import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import App from "./App";
import { DEFAULT_SETTINGS, useSessionStore } from "./lib/sessions";
import { UPDATE_LAST_CHECK_KEY } from "./lib/app-updater";

vi.mock("./components/AgentTabBar", () => ({
  AgentTabBar: ({ orientation }: { orientation: string }) => (
    <div data-testid="agent-tab-bar">{orientation}</div>
  ),
}));

vi.mock("./components/WorkspaceSidebar", () => ({
  WorkspaceSidebar: () => <div data-testid="workspace-sidebar" />,
}));

vi.mock("./components/MainPane", () => ({
  MainPane: () => <div data-testid="main-pane" />,
}));

vi.mock("./lib/desktop-api", () => ({
  startDesktopBridge: vi.fn(async () => vi.fn()),
}));

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
    sidebarCollapsed: false,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
  globalThis.__TAURI_MOCKS__.invoke.mockResolvedValue([]);
  globalThis.__TAURI_MOCKS__.updateCheck.mockReset();
  globalThis.__TAURI_MOCKS__.updateCheck.mockResolvedValue(null);
  globalThis.__TAURI_MOCKS__.processRelaunch.mockReset();
  localStorage.setItem(UPDATE_LAST_CHECK_KEY, String(Date.now()));
}

describe("App", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders default vertical tab layout without panel context errors", () => {
    render(<App />);

    expect(screen.getByTestId("main-pane")).toBeTruthy();
    expect(screen.getByTestId("agent-tab-bar").textContent).toContain("vertical");
    expect(screen.queryByLabelText("Sessions")).toBeNull();
    expect(screen.getByRole("main").getAttribute("data-tab-orientation")).toBe(
      "vertical",
    );
    const appBody = document.querySelector(".app-body");
    expect(
      appBody?.children[0].querySelector('[data-testid="agent-tab-bar"]'),
    ).toBeTruthy();
    expect(appBody?.children[1].getAttribute("aria-label")).toBe("Activity bar");
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

  test("persists sidebar collapse through the shared store", () => {
    render(<App />);
    expect(screen.getByTestId("workspace-sidebar")).toBeTruthy();

    fireEvent.click(screen.getByLabelText("Explorer"));

    expect(useSessionStore.getState().sidebarCollapsed).toBe(true);
    expect(screen.queryByTestId("workspace-sidebar")).toBeNull();
  });
});
