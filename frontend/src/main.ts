import "./main.css";
import { TerminalWS } from "./ws";
import {
  applyTerminalTheme,
  attachTerminalIO,
  clampTerminalFontSize,
  createTerminal,
  defaultTerminalFontSize,
  installTouchScroll,
  installViewportResize,
  loadStoredTerminalTheme,
  logCrampedPortraitCols,
  terminalFontSizeKey,
  terminalThemeKey
} from "./terminal";
import type { TerminalThemeName } from "./terminal";
import { ApprovalOverlay } from "./approval";
import { EventsWS } from "./events";
import type { EventEnvelope, ToastPayload } from "./events";
import { SoftKeyBar } from "./softkeys";
import { RelayE2E, relayE2EContentType } from "./e2e";
import { saveLastSessionID, SessionsListView } from "./sessions";
import { SnapshotsPanel } from "./snapshots";
import { refreshPushSubscription, subscribePushFromGesture } from "./push";
import { startFirstRunTour } from "./tour";
import { ApprovalWakeLock } from "./wake-lock";
import { installImagePaste } from "./image-paste";
import type { ImageUploadRequest } from "./image-paste";
import { SharePanel } from "./share";
import { ApprovalInboxPanel } from "./approval-inbox";
import { TerminalStatus } from "./terminal-status";
import { InterventionPanel } from "./intervention-panel";
import { SessionToolsPanel } from "./session-tools";

type SessionInfo = {
  session_id: string;
  ws_token: string;
  csrf_token: string;
  role: SessionRole;
};

type EventsInfo = {
  ws_token: string;
  csrf_token: string;
  role: SessionRole;
};

type SessionRole = "owner" | "viewer";

const termEl = requireElement("term");
const splash = requireElement("splash");
const approvalRoot = requireElement("approval-overlay");
const toolbar = requireElement("toolbar");
const sessionListRoot = requireElement("session-list");
const snapshotsRoot = requireElement("snapshots");
const softkeys = requireElement("softkeys");
const toast = requireElement("toast");
let theme = loadTheme();
applyDocumentTheme(theme);
const { term, fit } = createTerminal(termEl, theme);
const ws = new TerminalWS();
const events = new EventsWS();
const approvalWakeLock = new ApprovalWakeLock();
const approvals = new ApprovalOverlay(approvalRoot, approvalWakeLock);
let relayE2E: RelayE2E | undefined;
let sessionList: SessionsListView | undefined;
let snapshots: SnapshotsPanel | undefined;
let sharePanel: SharePanel | undefined;
let approvalInbox: ApprovalInboxPanel | undefined;
let interventionPanel: InterventionPanel | undefined;
let terminalInputEnabled = false;
let viewerMode = false;
let csrfToken = "";
const terminalStatus = new TerminalStatus();

attachTerminalIO(term, ws, () => terminalInputEnabled);
installViewportResize(term, fit, ws);
installTouchScroll(term, termEl);
const pinViewport = installViewportPinning(termEl);
installForegroundResume(pinViewport);
registerServiceWorker();
ws.addEventListener("data", (event) => {
  const data = (event as CustomEvent<ArrayBuffer>).detail;
  term.write(new Uint8Array(data));
});
ws.addEventListener("open", () => {
  splash.hidden = true;
  hideToast();
  terminalStatus.setConnected();
  fit.fit();
  logCrampedPortraitCols(term);
  ws.sendResize(term.rows, term.cols);
});
ws.addEventListener("close", () => terminalStatus.setDisconnected());
ws.addEventListener("reconnecting", (event) => {
  terminalStatus.setReconnecting((event as CustomEvent<number>).detail);
  showToast("Reconnecting...");
});
ws.addEventListener("recovered", (event) => {
  const recovery = (event as CustomEvent<{ mode: "replay" | "snapshot"; replay_bytes: number }>)
    .detail;
  terminalStatus.setRecovered(recovery);
  const message =
    recovery.mode === "snapshot"
      ? "Terminal restored from buffered output."
      : recovery.replay_bytes > 0
        ? "Terminal reconnected."
        : "Terminal connected.";
  showToast(message);
  window.setTimeout(() => {
    if (toast.textContent === message) {
      hideToast();
    }
  }, 1200);
});
events.addEventListener("event", (event) => {
  const envelope = (event as CustomEvent<EventEnvelope>).detail;
  if (!viewerMode) {
    approvals.handleEnvelope(envelope);
  }
  sessionList?.handleEnvelope(envelope);
  approvalInbox?.handleEnvelope(envelope);
  snapshots?.handleEnvelope(envelope);
});
events.addEventListener("toast", (event) => {
  const payload = (event as CustomEvent<EventEnvelope<ToastPayload>>).detail.payload;
  if (payload.message !== "") {
    showToast(payload.message);
  }
});

void boot();

async function boot(): Promise<void> {
  try {
    relayE2E = await RelayE2E.fromFragment();
    ws.setE2E(relayE2E);
    events.setE2E(relayE2E);
    approvals.setPostJSON(postJSON);
    const routeSession = routeSessionID();
    if (routeSession === null) {
      await showSessionsHome();
      return;
    }
    const info = await sessionInfo(routeSession);
    csrfToken = info.csrf_token;
    viewerMode = info.role === "viewer";
    terminalInputEnabled = !viewerMode;
    await relayE2E?.bindSession(info.ws_token);
    refreshPushOnOpen();
    saveLastSessionID(info.session_id);
    showTerminalChrome(viewerMode);
    snapshots = new SnapshotsPanel(
      snapshotsRoot,
      info.session_id,
      getJSON,
      postJSON,
      navigateToSession,
      showToast
    );
    const imagePaste = viewerMode
      ? undefined
      : installImagePaste({
          root: termEl,
          uploadImage,
          sendText: (path) => ws.sendText(path),
          showToast,
          focus: () => term.focus()
        });
    sharePanel = viewerMode
      ? undefined
      : new SharePanel(document.body, info.session_id, getJSON, postJSON, showToast);
    interventionPanel = viewerMode
      ? undefined
      : new InterventionPanel(
          document.body,
          info.session_id,
          postJSON,
          getJSON,
          () => term.focus(),
          (target) => {
            if (target === "mac") {
              ws.close();
            }
          }
        );
    const tools = new SessionToolsPanel(document.body, [
      { label: "Snapshots", action: () => snapshots?.toggle() },
      ...(sharePanel === undefined ? [] : [{ label: "Share", action: () => sharePanel.open() }])
    ]);
    installControls(toolbar, tools, interventionPanel);
    new SoftKeyBar({
      root: softkeys,
      sendBytes: (data) => ws.sendBinary(data),
      sendText: (data) => ws.sendText(data),
      pageUp: () => postControl(info.session_id, "page_up"),
      pageDown: () => postControl(info.session_id, "page_down"),
      focus: () => term.focus(),
      getTheme: () => theme,
      setTheme: setTheme,
      decreaseFontSize: () => changeTerminalFontSize(-1),
      increaseFontSize: () => changeTerminalFontSize(1),
      pasteImage: () => imagePaste?.pasteFromClipboard() ?? Promise.resolve(false),
      readOnly: viewerMode
    });
    connectTerminal(info);
    events.connect(eventsURL(info.ws_token));
    if (!viewerMode) {
      startFirstRunTour();
    }
  } catch {
    splash.textContent = "session unavailable";
  }
}

async function showSessionsHome(): Promise<void> {
  viewerMode = false;
  terminalInputEnabled = false;
  showListChrome();
  approvalInbox = new ApprovalInboxPanel(document.body, getJSON, postJSON, showToast);
  const hostControls = document.createElement("div");
  hostControls.className = "session-list-header-controls";
  hostControls.append(approvalInbox.element);
  sessionList = new SessionsListView(
    sessionListRoot,
    getJSON,
    navigateToSession,
    postJSON,
    showToast,
    hostControls
  );
  const setOffline = (offline: boolean) => {
    approvalInbox?.setOffline(offline);
  };
  window.addEventListener("offline", () => setOffline(true));
  window.addEventListener("online", () => setOffline(false));
  if (!navigator.onLine) {
    setOffline(true);
  }
  await connectSessionListEvents();
  await sessionList.load();
  refreshPushOnOpen();
  splash.hidden = true;
  startFirstRunTour();
}

async function sessionInfo(sessionID: string): Promise<SessionInfo> {
  return getJSON<SessionInfo>(`/session-info?session_id=${encodeURIComponent(sessionID)}`);
}

async function connectSessionListEvents(): Promise<void> {
  try {
    const info = await getJSON<EventsInfo>("/session-info?events=1");
    csrfToken = info.csrf_token;
    viewerMode = info.role === "viewer";
    await relayE2E?.bindSession(info.ws_token);
    events.connect(eventsURL(info.ws_token));
  } catch {
    return;
  }
}

function routeSessionID(): string | null {
  const match = /^\/s\/([^/]+)$/.exec(window.location.pathname);
  if (match !== null) {
    return decodeURIComponent(match[1]);
  }
  const querySession = new URLSearchParams(window.location.search).get("session_id");
  if (querySession !== null && querySession.trim() !== "") {
    return querySession;
  }
  return null;
}

async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch(path, { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error(`${path} ${response.status}`);
  }
  return (await response.json()) as T;
}

function wsURL(token: string): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/ws/pty?token=${encodeURIComponent(token)}`;
}

function eventsURL(token: string): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/ws/events?token=${encodeURIComponent(token)}`;
}

function installControls(
  root: HTMLElement,
  tools: SessionToolsPanel,
  intervention: InterventionPanel | undefined
): void {
  const controls: HTMLElement[] = [
    terminalStatus.element,
    tools.element,
    controlButton("PUSH", () => enablePush())
  ];
  if (intervention !== undefined) {
    controls.unshift(intervention.element);
  }
  root.replaceChildren(...controls);
  // A phone-width toolbar has more controls than can fit. Always begin at the
  // first control and allow a deliberate horizontal swipe to reach the rest;
  // flex-end previously clipped the leading controls with no way to recover them.
  root.scrollLeft = 0;
}

function controlButton(label: string, action: () => void, tourID = ""): HTMLButtonElement {
  const el = document.createElement("button");
  let firedAt = 0;
  let skipNextClick = false;
  const fire = (event: Event) => {
    event.preventDefault();
    if (Date.now() - firedAt < 250) {
      return;
    }
    firedAt = Date.now();
    skipNextClick = true;
    action();
    term.focus();
  };
  el.type = "button";
  el.className = "control-button";
  el.textContent = label;
  if (tourID !== "") {
    el.dataset.tour = tourID;
  }
  el.addEventListener("pointerdown", fire);
  el.addEventListener("touchstart", fire, { passive: false });
  el.addEventListener("click", (event) => {
    event.preventDefault();
    if (skipNextClick) {
      skipNextClick = false;
      return;
    }
    action();
    term.focus();
  });
  return el;
}

function enablePush(): void {
  void (async () => {
    try {
      await subscribePushFromGesture(postJSON);
      showToast("Push enabled.");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Push unavailable.");
    }
  })();
}

function refreshPushOnOpen(): void {
  void refreshPushSubscription(postJSON).catch(() => {});
}

async function postControl(sessionID: string, action: string): Promise<void> {
  try {
    const response = await postJSON("/control", { session_id: sessionID, action });
    if (!response.ok) {
      const text = await response.text();
      let body = {} as { message?: unknown };
      try {
        body = text === "" ? {} : (JSON.parse(text) as { message?: unknown });
      } catch {
        body = {};
      }
      showToast(
        typeof body.message === "string"
          ? body.message
          : text.trim() || `control ${response.status}`
      );
    }
  } catch {
    showToast("Control failed.");
  }
}

function connectTerminal(info: SessionInfo): void {
  ws.connect(wsURL(info.ws_token), info.session_id, 0);
}

function navigateToSession(sessionID: string): void {
  if (sessionID.trim() === "") {
    return;
  }
  saveLastSessionID(sessionID);
  window.location.href = `/s/${encodeURIComponent(sessionID)}`;
}

async function postJSON(path: string, body: Record<string, unknown>): Promise<Response> {
  return sendJSON("POST", path, body);
}

async function uploadImage(request: ImageUploadRequest): Promise<string> {
  const response = await postJSON("/attachments/images", request);
  if (!response.ok) {
    throw new Error((await response.text()).trim() || `upload ${response.status}`);
  }
  const body = (await response.json()) as { path?: unknown };
  if (typeof body.path !== "string" || body.path === "") {
    throw new Error("upload path missing");
  }
  return body.path;
}

async function sendJSON(
  method: "POST" | "PUT",
  path: string,
  body: Record<string, unknown>
): Promise<Response> {
  const raw = JSON.stringify(body);
  const routePath = new URL(path, window.location.href).pathname;
  const csrfHeaders = csrfToken === "" ? {} : { "X-Onibi-CSRF": csrfToken };
  if (relayE2E === undefined) {
    return fetch(path, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", ...csrfHeaders },
      body: raw
    });
  }
  const channel = `http:${method}:${routePath}`;
  const sealed = await relayE2E.sealHTTPRequest(raw, channel);
  const response = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: { "Content-Type": relayE2EContentType, ...csrfHeaders },
    body: sealed.body
  });
  return relayE2E.openHTTPResponse(response, channel, sealed.streamID);
}

function setTheme(next: TerminalThemeName): void {
  theme = next;
  window.localStorage.setItem(terminalThemeKey, next);
  applyDocumentTheme(next);
  applyTerminalTheme(term, next);
}

function changeTerminalFontSize(delta: number): void {
  const current =
    typeof term.options.fontSize === "number" ? term.options.fontSize : defaultTerminalFontSize;
  const next = clampTerminalFontSize(current + delta);
  term.options.fontSize = next;
  window.localStorage.setItem(terminalFontSizeKey, String(next));
  fit.fit();
  logCrampedPortraitCols(term);
  ws.sendResize(term.rows, term.cols);
}

function loadTheme(): TerminalThemeName {
  return loadStoredTerminalTheme(window.localStorage);
}

function applyDocumentTheme(next: TerminalThemeName): void {
  document.documentElement.dataset.theme = next;
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", documentThemeColor(next));
}

function documentThemeColor(next: TerminalThemeName): string {
  if (next === "light") {
    return "#f6f8fa";
  }
  return "#090b0f";
}

function showToast(message: string): void {
  toast.textContent = message;
  toast.hidden = false;
}

function hideToast(): void {
  toast.hidden = true;
}

function showListChrome(): void {
  toolbar.hidden = true;
  snapshotsRoot.hidden = true;
  filesRoot.hidden = true;
  termEl.hidden = true;
  softkeys.hidden = true;
  approvalRoot.hidden = true;
  sessionListRoot.hidden = false;
}

function showTerminalChrome(readOnly: boolean): void {
  toolbar.hidden = false;
  termEl.hidden = false;
  softkeys.hidden = false;
  approvalRoot.hidden = readOnly;
  sessionListRoot.hidden = true;
}

function installViewportPinning(root: HTMLElement): () => void {
  const viewport = window.visualViewport;
  let frame = 0;
  const pin = () => {
    window.cancelAnimationFrame(frame);
    frame = window.requestAnimationFrame(() => {
      const height = viewport?.height ?? window.innerHeight;
      const offsetTop = viewport?.offsetTop ?? 0;
      document.documentElement.style.setProperty("--visual-viewport-height", `${height}px`);
      const keyboardBottom = Math.max(0, window.innerHeight - height - offsetTop);
      document.documentElement.style.setProperty("--keyboard-bottom", `${keyboardBottom}px`);
      const cursor = root.querySelector<HTMLElement>(".xterm-helper-textarea");
      cursor?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
  };
  viewport?.addEventListener("resize", pin);
  viewport?.addEventListener("scroll", pin);
  window.addEventListener("resize", pin);
  window.addEventListener("orientationchange", pin);
  pin();
  return pin;
}

function installForegroundResume(pinViewport: () => void): void {
  const resume = (announce: boolean) => {
    if (document.visibilityState === "hidden") {
      return;
    }
    pinViewport();
    fit.fit();
    logCrampedPortraitCols(term);
    ws.sendResize(term.rows, term.cols);
    ws.resume();
    events.resume();
    if (!announce) {
      return;
    }
    showToast("Resuming session...");
    window.setTimeout(() => {
      if (toast.textContent === "Resuming session...") {
        hideToast();
      }
    }, 1200);
  };
  document.addEventListener("visibilitychange", () => resume(true));
  window.addEventListener("pageshow", () => resume(true));
  window.addEventListener("online", () => resume(true));
  window.addEventListener("orientationchange", () => resume(false));
}

function registerServiceWorker(): void {
  if ("serviceWorker" in navigator && window.isSecureContext) {
    void navigator.serviceWorker.register("/sw.js");
  }
}

function requireElement(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`missing #${id}`);
  }
  return el;
}
