import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ApprovalAuditView } from "./ApprovalAuditView";

describe("ApprovalAuditView", () => {
  beforeEach(() => {
    window.localStorage.setItem("onibi.token", "test-token");
    window.localStorage.setItem("onibi.port", "17893");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => [
          {
            protocol_version: "1.0",
            approval_id: "approval-1",
            machine_id: "machine",
            session_id: "session",
            agent: "claude-code",
            tool: "Bash",
            input: { command: "rm -rf node_modules" },
            cwd: "/repo",
            decision: "allow",
            updatedInput: { command: "mv node_modules node_modules.bak" },
            reason: "edited from mobile",
            decided_by: "mobile",
            created_at: 1000,
            decided_at: 2000,
          },
          {
            protocol_version: "1.0",
            approval_id: "approval-2",
            machine_id: "machine",
            session_id: "session",
            agent: "goose",
            tool: "Shell",
            input: { command: "git status" },
            cwd: "/repo",
            decision: "deny",
            reason: "not now",
            decided_by: "desktop",
            created_at: 3000,
            decided_at: 4000,
          },
        ],
      })),
    );
  });

  afterEach(() => {
    window.localStorage.clear();
    vi.unstubAllGlobals();
  });

  test("renders approval history and filters edited approvals", async () => {
    render(<ApprovalAuditView />);

    expect(await screen.findByText("claude-code · Bash")).toBeTruthy();
    expect(screen.getByText("goose · Shell")).toBeTruthy();
    expect(screen.getByText("Proposed input")).toBeTruthy();
    expect(screen.getByText("Final input")).toBeTruthy();
    expect(screen.getByText("- rm -rf node_modules")).toBeTruthy();
    expect(screen.getByText("+ mv node_modules node_modules.bak")).toBeTruthy();
    expect(screen.getByText(/1 total · 1 allowed · 0 denied · 1 edited/)).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Edited" }));
    expect(screen.getByText("1 edited")).toBeTruthy();
    expect(screen.queryByText("goose · Shell")).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: "All" }));
    fireEvent.change(screen.getByLabelText("Filter approvals by agent"), {
      target: { value: "goose" },
    });
    expect(screen.getByText("goose · Shell")).toBeTruthy();
    expect(screen.queryByText("claude-code · Bash")).toBeNull();

    fireEvent.change(screen.getByLabelText("Filter approvals by tool"), {
      target: { value: "Shell" },
    });
    expect(screen.getByText("1 denied")).toBeTruthy();

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/approval/history?limit=500",
        expect.objectContaining({
          headers: { authorization: "Bearer test-token" },
        }),
      );
    });
  });
});
