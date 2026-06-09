import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";
import { WorkspaceRightDock } from "./WorkspaceRightDock";

vi.mock("./WorkspaceSidebar", () => ({
  WorkspaceSidebarContent: ({ view }: { view: string }) => (
    <div data-testid="dock-content">{view}</div>
  ),
}));

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    activeSidebarView: "files",
    rightDockView: "files",
    rightDockMode: "expanded",
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
}

describe("WorkspaceRightDock", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders active files content when expanded", () => {
    render(<WorkspaceRightDock />);

    expect(screen.getByLabelText("Workspace dock").getAttribute("data-mode")).toBe(
      "expanded",
    );
    expect(screen.getByTestId("dock-content").textContent).toBe("files");
  });

  test("compresses and expands with selected view", () => {
    render(<WorkspaceRightDock />);

    fireEvent.click(screen.getByLabelText("Compress workspace dock"));
    expect(useSessionStore.getState().rightDockMode).toBe("compressed");
    expect(screen.queryByTestId("dock-content")).toBeNull();

    fireEvent.click(screen.getByLabelText("Search"));
    expect(useSessionStore.getState().rightDockView).toBe("search");
    expect(useSessionStore.getState().rightDockMode).toBe("expanded");
    expect(screen.getByTestId("dock-content").textContent).toBe("search");
  });

  test("opens source control in the dock", () => {
    render(<WorkspaceRightDock />);

    fireEvent.click(screen.getByLabelText("Source Control"));

    expect(useSessionStore.getState().rightDockView).toBe("source-control");
    expect(useSessionStore.getState().activeSidebarView).toBe("source-control");
    expect(screen.getByTestId("dock-content").textContent).toBe("source-control");
  });
});
