import { describe, expect, test } from "vitest";
import {
  buildDecisionBody,
  candidateBaseUrls,
  chooseBaseUrl,
  commandText,
  connectionStateMessage,
  emergencyStopRequest,
  installStateBody,
  installStateTitle,
  parsePairingInput,
  reconnectDelay,
  swipeDecision,
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
    expect(buildDecisionBody(approval, "deny", undefined, "too broad")).toEqual({
      decision: "deny",
      by: "mobile",
      reason: "too broad",
    });
  });

  test("orders paired transports for phone reachability", () => {
    expect(candidateBaseUrls({
      baseUrl: "http://127.0.0.1:17893",
      token: "secret",
      deviceId: "device",
      machineId: "machine",
      scope: "full",
      transports: [
        { name: "loopback", url: "http://127.0.0.1:17893/" },
        { name: "lan", url: "https://192.168.1.10:17893/" },
        { name: "cloudflared", url: "https://demo.trycloudflare.com/" },
      ],
    })).toEqual([
      "http://127.0.0.1:17893",
      "https://192.168.1.10:17893",
      "https://demo.trycloudflare.com",
    ]);
  });

  test("builds the emergency stop request", () => {
    expect(emergencyStopRequest()).toEqual({
      method: "POST",
      body: "{}",
    });
  });

  test("caps websocket reconnect delay", () => {
    expect(reconnectDelay(0)).toBe(1000);
    expect(reconnectDelay(8)).toBe(30000);
  });

  test("parses read-only pairing scope", () => {
    const payload = parsePairingInput(
      JSON.stringify({
        token: "spectator",
        scope: "read-only",
        host: "127.0.0.1",
        port: 17893,
      }),
    );
    expect(payload.scope).toBe("read-only");
  });

  test("maps horizontal swipes to decisions", () => {
    expect(swipeDecision(120, 320)).toBe("allow");
    expect(swipeDecision(-120, 320)).toBe("deny");
    expect(swipeDecision(40, 320)).toBeNull();
  });

  test("describes realtime fallback and install state", () => {
    expect(connectionStateMessage("fallback", "lan https://phone")).toContain(
      "Trying fallback transport",
    );
    expect(connectionStateMessage("closed", "cloudflared")).toContain("Cached approvals");
    expect(installStateTitle({ standalone: false, ios: false, installable: true })).toBe(
      "Ready to install.",
    );
    expect(installStateBody({ standalone: true, ios: false, installable: false })).toContain(
      "deep links",
    );
  });
});
