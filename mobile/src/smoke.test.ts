import { describe, expect, test } from "vitest";
import {
  buildDecisionBody,
  chooseBaseUrl,
  commandText,
  parsePairingInput,
  reconnectDelay,
  type Approval,
} from "./App";

describe("mobile pairing and approval helpers", () => {
  test("parses pairing JSON and chooses a phone-reachable transport", () => {
    const payload = parsePairingInput(
      JSON.stringify({
        token: "secret",
        transports: [
          { name: "loopback", url: "http://127.0.0.1:17893/" },
          { name: "cloudflared", url: "https://demo.trycloudflare.com/" },
        ],
      }),
    );

    expect(payload.token).toBe("secret");
    expect(chooseBaseUrl(payload)).toBe("https://demo.trycloudflare.com");
  });

  test("builds edited allow decision payloads", () => {
    const approval: Approval = {
      approval_id: "approval",
      machine_id: "machine",
      session_id: "session",
      agent: "claude-code",
      tool: "Bash",
      input: { command: "rm -rf node_modules" },
      cwd: "/repo",
      created_at: 1,
    };

    expect(commandText(approval.input)).toBe("rm -rf node_modules");
    expect(buildDecisionBody(approval, "allow", "echo skipped")).toEqual({
      decision: "allow",
      by: "mobile",
      reason: "edited from mobile",
      updatedInput: { command: "echo skipped" },
    });
  });

  test("caps websocket reconnect delay", () => {
    expect(reconnectDelay(0)).toBe(1000);
    expect(reconnectDelay(8)).toBe(30000);
  });
});
