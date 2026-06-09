import { describe, expect, test } from "vitest";
import {
  buildDecisionBody,
  buildPaneTargetOptions,
  candidateBaseUrls,
  chooseBaseUrl,
  commandText,
  connectionStateMessage,
  emergencyStopRequest,
  installStateBody,
  installStateTitle,
  needsRemoteConfirmation,
  parsePairingInput,
  reconnectDelay,
  resolveRemoteTarget,
  sendRemotePresetRequest,
  sendRemoteTextRequest,
  swipeDecision,
  type Approval,
  type PaneTarget,
  type RunEvent,
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

  test("resolves remote pane targets by explicit, approval, recent, then singleton", () => {
    const targets: PaneTarget[] = [
      {
        paneId: "pane-a",
        sessionId: "pane-a",
        label: "A",
        status: "working",
        trustMode: "full-access",
      },
      {
        paneId: "pane-b",
        sessionId: "pane-b",
        label: "B",
        status: "blocked",
        trustMode: "approval-required",
      },
    ];
    const pending: Approval[] = [
      {
        approval_id: "approval-b",
        machine_id: "machine",
        session_id: "pane-b",
        agent: "claude-code",
        tool: "Bash",
        input: {},
        cwd: "/repo",
        created_at: 1,
      },
    ];
    const recent: RunEvent[] = [{ session_id: "pane-a", kind: "done", payload: {}, ts: 2 }];

    expect(resolveRemoteTarget("pane-a", targets, recent, pending, "approval-b")).toBe("pane-a");
    expect(resolveRemoteTarget("", targets, recent, pending, "approval-b")).toBe("pane-b");
    expect(resolveRemoteTarget("", targets, recent, [], null)).toBe("pane-a");
    expect(resolveRemoteTarget("", [targets[0]], [], [], null)).toBe("pane-a");
  });

  test("builds fallback pane targets from pending, recent, and terminal output", () => {
    const targets = buildPaneTargetOptions(
      [],
      [{ session_id: "recent-session", kind: "event", payload: {}, ts: 1 }],
      { "terminal-session": [""] },
      [
        {
          approval_id: "approval",
          machine_id: "machine",
          session_id: "approval-session",
          agent: "codex",
          tool: "Bash",
          input: {},
          cwd: "/repo",
          created_at: 1,
        },
      ],
    );

    expect(targets.map((target) => target.paneId)).toEqual([
      "approval-session",
      "recent-session",
      "terminal-session",
    ]);
  });

  test("builds remote input request bodies and confirmation state", () => {
    const approvalRequired: PaneTarget = {
      paneId: "pane",
      sessionId: "pane",
      label: "Pane",
      status: "working",
      trustMode: "approval-required",
    };
    const fullAccess: PaneTarget = { ...approvalRequired, trustMode: "full-access" };

    expect(sendRemoteTextRequest("continue", true, false)).toEqual({
      text: "continue",
      sendEnter: true,
      confirmed: false,
    });
    expect(sendRemotePresetRequest("interrupt", true)).toEqual({
      preset: "interrupt",
      confirmed: true,
    });
    expect(needsRemoteConfirmation(approvalRequired, false)).toBe(true);
    expect(needsRemoteConfirmation(fullAccess, false)).toBe(false);
    expect(needsRemoteConfirmation(fullAccess, true)).toBe(true);
  });
});
