import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  dispatchOnibiNotification,
  notificationEvents,
  type OnibiNotificationToast,
} from "./notifications";
import { DEFAULT_SETTINGS, useSessionStore } from "./sessions";

const audioPlay = vi.fn(async () => undefined);
const audioConstructor = vi.fn(function AudioMock(this: { play: typeof audioPlay }) {
  this.play = audioPlay;
});

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    terminalLayout: null,
    activeTerminalPaneId: null,
    maximizedTerminalPaneId: null,
    arrangements: [],
    workspaces: [],
    selectedFile: null,
    sessionEvents: [],
    settings: DEFAULT_SETTINGS,
  });
}

function installNotificationMock(permission: NotificationPermission) {
  const requestPermission = vi.fn(async () => permission);
  const notificationConstructor = vi.fn();
  Object.defineProperty(notificationConstructor, "permission", {
    value: permission,
    configurable: true,
  });
  Object.defineProperty(notificationConstructor, "requestPermission", {
    value: requestPermission,
    configurable: true,
  });
  Object.defineProperty(window, "Notification", {
    value: notificationConstructor,
    configurable: true,
  });
  return { notificationConstructor, requestPermission };
}

describe("notifications", () => {
  beforeEach(() => {
    resetStore();
    audioPlay.mockClear();
    audioConstructor.mockClear();
    Object.defineProperty(globalThis, "Audio", {
      value: audioConstructor,
      configurable: true,
    });
  });

  test("delivers in-app toasts by default", async () => {
    const toasts: OnibiNotificationToast[] = [];
    const listener = (event: Event) =>
      toasts.push((event as CustomEvent<OnibiNotificationToast>).detail);
    window.addEventListener(notificationEvents.toast, listener);

    try {
      const result = await dispatchOnibiNotification({
        title: "Build failed",
        body: "tests failed",
        kind: "request",
        source: "trigger",
        sessionId: "pty-1",
        agent: "codex",
      });

      expect(result).toMatchObject({
        delivered: true,
        soundPlayed: false,
        suppressed: false,
        delivery: "in_app",
      });
      expect(toasts).toContainEqual(
        expect.objectContaining({
          title: "Build failed",
          body: "tests failed",
          kind: "request",
          sessionId: "pty-1",
          agent: "codex",
        }),
      );
    } finally {
      window.removeEventListener(notificationEvents.toast, listener);
    }
  });

  test("uses system notifications when configured", async () => {
    const { notificationConstructor, requestPermission } =
      installNotificationMock("granted");
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, notificationDelivery: "system" },
    });

    const result = await dispatchOnibiNotification({
      title: "Approval needed",
      body: "Bash wants to run",
      kind: "request",
      source: "trigger",
    });

    expect(result.delivered).toBe(true);
    expect(requestPermission).not.toHaveBeenCalled();
    expect(notificationConstructor).toHaveBeenCalledWith("Approval needed", {
      body: "Bash wants to run",
    });
  });

  test("emits terminal notices when configured", async () => {
    const notices: unknown[] = [];
    const listener = (event: Event) => notices.push((event as CustomEvent).detail);
    window.addEventListener(notificationEvents.terminalNotice, listener);
    useSessionStore.setState({
      settings: { ...DEFAULT_SETTINGS, notificationDelivery: "terminal" },
    });

    try {
      const result = await dispatchOnibiNotification({
        title: "Terminal says hi",
        body: "hello",
        kind: "info",
        source: "pty",
        sessionId: "pty-1",
      });

      expect(result.delivered).toBe(true);
      expect(notices).toContainEqual(
        expect.objectContaining({
          title: "Terminal says hi",
          body: "hello",
          sessionId: "pty-1",
        }),
      );
    } finally {
      window.removeEventListener(notificationEvents.terminalNotice, listener);
    }
  });

  test("suppresses foreground session notifications", async () => {
    useSessionStore.setState({
      activeSessionId: "pty-1",
      selectedFile: null,
      settings: { ...DEFAULT_SETTINGS, soundAlertsEnabled: true },
    });

    const result = await dispatchOnibiNotification({
      title: "Foreground",
      kind: "request",
      source: "trigger",
      sessionId: "pty-1",
      agent: "codex",
    });

    expect(result).toMatchObject({
      delivered: false,
      soundPlayed: false,
      suppressed: true,
    });
    expect(audioConstructor).not.toHaveBeenCalled();
  });

  test("respects per-agent sound muting", async () => {
    useSessionStore.setState({
      settings: {
        ...DEFAULT_SETTINGS,
        soundAlertsEnabled: true,
        soundRequestPath: "/tmp/request.mp3",
        soundAgents: { codex: false, shell: true },
      },
    });

    const muted = await dispatchOnibiNotification({
      title: "Muted",
      kind: "request",
      source: "trigger",
      agent: "codex",
    });
    const allowed = await dispatchOnibiNotification({
      title: "Allowed",
      kind: "request",
      source: "trigger",
      agent: "shell",
    });

    expect(muted.soundPlayed).toBe(false);
    expect(allowed.soundPlayed).toBe(true);
    expect(audioConstructor).toHaveBeenCalledTimes(1);
    expect(audioConstructor).toHaveBeenCalledWith("asset:///tmp/request.mp3");
  });
});
