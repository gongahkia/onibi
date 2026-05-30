import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { EmptyState } from "./EmptyState";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

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
}

describe("EmptyState", () => {
  beforeEach(() => {
    resetStore();
  });

  test("renders the start prompt without landing-page help copy", () => {
    render(<EmptyState />);
    expect(screen.getByText("Start")).toBeTruthy();
    expect(screen.queryByText("Help")).toBeNull();
    expect(screen.queryByText("Local AI agent cockpit. Open a folder, start a session.")).toBeNull();
  });

  test("clones a repository into a workspace", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "git_clone_repository") {
        return { path: "/repo/onibi", name: "onibi" };
      }
      return null;
    });

    render(<EmptyState />);
    fireEvent.click(screen.getByText("Clone Git Repository..."));
    fireEvent.change(screen.getByLabelText("Repository URL"), {
      target: { value: "https://github.com/gongahkia/onibi.git" },
    });
    fireEvent.change(screen.getByLabelText("Destination"), {
      target: { value: "/repo" },
    });
    fireEvent.click(screen.getByText("Clone"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "git_clone_repository",
        {
          remote: "https://github.com/gongahkia/onibi.git",
          destinationParent: "/repo",
          name: "onibi",
        },
      );
    });
    expect(useSessionStore.getState().workspaces[0]).toMatchObject({
      path: "/repo/onibi",
      name: "onibi",
    });
  });
});
