import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test } from "vitest";
import { TitleBar } from "./TitleBar";
import { COMMAND_PALETTE_USED_KEY } from "../lib/command-palette-discovery";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

describe("TitleBar", () => {
  beforeEach(() => {
    useSessionStore.setState({
      hydrated: true,
      sessions: [
        {
          id: "pty-1",
          agent: "claude-code",
          workspaceId: "workspace:/repo",
          title: "Claude",
          status: "awaiting-approval",
          createdAt: 1,
          pendingApprovals: ["approval-1"],
        },
      ],
      activeSessionId: "pty-1",
      selectedFile: null,
      workspaces: [{ id: "workspace:/repo", path: "/repo", name: "repo" }],
      settings: DEFAULT_SETTINGS,
    });
    localStorage.removeItem(COMMAND_PALETTE_USED_KEY);
  });

  test("pulses the active agent dot for non-suppressed approval attention", () => {
    render(<TitleBar />);

    const status = screen.getByLabelText("awaiting-approval");
    act(() => {
      window.dispatchEvent(
        new CustomEvent("onibi:approval-attention", {
          detail: { escalate: true, sessionId: "pty-1" },
        }),
      );
    });

    expect(status.classList.contains("approval-attention")).toBe(true);
  });

  test("ignores suppressed approval attention", () => {
    render(<TitleBar />);

    const status = screen.getByLabelText("awaiting-approval");
    act(() => {
      window.dispatchEvent(
        new CustomEvent("onibi:approval-attention", {
          detail: { escalate: false, sessionId: "pty-1" },
        }),
      );
    });

    expect(status.classList.contains("approval-attention")).toBe(false);
  });

  test("marks the command palette affordance as discovered once used", () => {
    render(<TitleBar />);

    const button = screen.getByRole("button", { name: "Open command palette" });
    expect(button.classList.contains("discovered")).toBe(false);

    fireEvent.click(button);

    expect(localStorage.getItem(COMMAND_PALETTE_USED_KEY)).toBe("1");
    expect(button.classList.contains("discovered")).toBe(true);
  });
});
