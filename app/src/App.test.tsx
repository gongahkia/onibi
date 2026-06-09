import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import App from "./App";
import { DEFAULT_SETTINGS, useSessionStore } from "./lib/sessions";
import { UPDATE_LAST_CHECK_KEY } from "./lib/app-updater";

vi.mock("./components/AgentTabBar", () => ({
  AgentTabBar: ({ orientation }: { orientation: string }) => (
    <div data-testid="agent-tab-bar">{orientation}</div>
  ),
}));

vi.mock("./components/WorkspaceRightDock", () => ({
  WorkspaceRightDock: () => <div data-testid="workspace-right-dock" />,
}));

vi.mock("./components/MainPane", () => ({
  MainPane: () => <div data-testid="main-pane" />,
}));

vi.mock("./components/FocusHud", () => ({
  FocusHud: () => <div data-testid="focus-hud" />,
}));

vi.mock("./components/TransportWatcher", () => ({
  TransportWatcher: () => null,
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
    activeSidebarView: "files",
    rightDockView: "files",
    rightDockMode: "expanded",
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

  afterEach(() => {
    vi.unstubAllGlobals();
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
    expect(screen.queryByLabelText("Activity bar")).toBeNull();
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

  test("uses configured narrow layout threshold", () => {
    Object.defineProperty(window, "innerWidth", {
      value: 900,
      configurable: true,
    });
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        mobileLayoutThresholdPx: 960,
      },
    });

    render(<App />);

    expect(
      document.querySelector(".app-frame")?.getAttribute("data-mobile-layout"),
    ).toBe("true");
  });

  test("keeps recent files in the right dock by default", () => {
    render(<App />);
    expect(screen.getByTestId("workspace-right-dock")).toBeTruthy();
    expect(screen.queryByLabelText("Explorer")).toBeNull();
    expect(screen.queryByLabelText("Source Control")).toBeNull();
  });

  test("refreshes system theme when the OS color scheme changes", () => {
    const listeners = new Set<EventListenerOrEventListenerObject>();
    let prefersLight = false;
    const mediaQueryList = {
      get matches() {
        return prefersLight;
      },
      media: "(prefers-color-scheme: light)",
      onchange: null,
      addEventListener: vi.fn(
        (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.add(listener);
        },
      ),
      removeEventListener: vi.fn(
        (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.delete(listener);
        },
      ),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    } as unknown as MediaQueryList;
    vi.stubGlobal("matchMedia", vi.fn(() => mediaQueryList));
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, theme: "system" },
    });

    render(<App />);

    expect(document.documentElement.dataset.theme).toBe("vscode-dark-plus");

    act(() => {
      prefersLight = true;
      listeners.forEach((listener) => {
        if (typeof listener === "function") {
          listener(new Event("change"));
        } else {
          listener.handleEvent(new Event("change"));
        }
      });
    });

    expect(document.documentElement.dataset.theme).toBe("vscode-light-plus");
  });

  test("refreshes explicit theme pairs when the OS color scheme changes", () => {
    const listeners = new Set<EventListenerOrEventListenerObject>();
    let prefersLight = false;
    const mediaQueryList = {
      get matches() {
        return prefersLight;
      },
      media: "(prefers-color-scheme: light)",
      onchange: null,
      addEventListener: vi.fn(
        (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.add(listener);
        },
      ),
      removeEventListener: vi.fn(
        (_type: string, listener: EventListenerOrEventListenerObject) => {
          listeners.delete(listener);
        },
      ),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    } as unknown as MediaQueryList;
    vi.stubGlobal("matchMedia", vi.fn(() => mediaQueryList));
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, theme: "light:github-light,dark:tokyo-night" },
    });

    render(<App />);

    expect(document.documentElement.dataset.theme).toBe("tokyo-night");

    act(() => {
      prefersLight = true;
      listeners.forEach((listener) => {
        if (typeof listener === "function") {
          listener(new Event("change"));
        } else {
          listener.handleEvent(new Event("change"));
        }
      });
    });

    expect(document.documentElement.dataset.theme).toBe("github-light");
  });
});
