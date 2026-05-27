import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { ApprovalModal } from "./ApprovalModal";
import type { ApprovalPendingMessage } from "../lib/approval-client";

const pending: ApprovalPendingMessage = {
  type: "approval-pending",
  protocol_version: "1.0",
  approval_id: "01HAPPROVAL0000000000000",
  machine_id: "01HMACHINE00000000000000",
  session_id: "01HSESSION00000000000000",
  agent: "claude-code",
  tool: "Bash",
  input: { command: "rm -rf node_modules" },
  cwd: "/tmp/onibi",
};

describe("ApprovalModal", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn(async () => ({ ok: true, status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  test("renders a pending approval and allows it", async () => {
    const onResolved = vi.fn();
    render(<ApprovalModal initialPending={pending} token="test-token" onResolved={onResolved} />);

    expect(screen.getByRole("dialog", { name: "Approval request" })).toBeTruthy();
    expect(screen.getByText("claude-code")).toBeTruthy();
    expect(screen.getByText("rm -rf node_modules")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Allow" }));

    await waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        "http://127.0.0.1:17893/v1/approval/01HAPPROVAL0000000000000/decide",
        expect.objectContaining({
          method: "POST",
          headers: expect.objectContaining({
            authorization: "Bearer test-token",
          }),
        }),
      );
    });
    expect(onResolved).toHaveBeenCalledWith("01HAPPROVAL0000000000000");
  });

  test("submits edited Bash input as updatedInput", async () => {
    render(<ApprovalModal initialPending={pending} token="test-token" />);
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Edited tool input"), {
      target: { value: "echo skipped" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Submit Edit" }));

    await waitFor(() => {
      const [, request] = fetchMock.mock.calls[0];
      expect(JSON.parse(request.body as string)).toEqual({
        decision: "allow",
        updatedInput: { command: "echo skipped" },
      });
    });
  });
});
