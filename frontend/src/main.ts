import "./main.css";
import { TerminalWS } from "./ws";
import { applyTerminalTheme, attachTerminalIO, createTerminal, installTouchScroll, installViewportResize } from "./terminal";
import type { TerminalThemeName } from "./terminal";
import { AnomalyOverlay } from "./anomaly";
import { ApprovalOverlay } from "./approval";
import { EventsWS } from "./events";
import type { EventEnvelope, ToastPayload } from "./events";
import { SoftKeyBar } from "./softkeys";
import { RelayE2E } from "./e2e";
import { saveLastSessionID, SessionsListView, SessionsPanel } from "./sessions";
import { SnapshotsPanel } from "./snapshots";
import { TimelinePanel } from "./timeline";
import { WorkspaceSwitcher } from "./workspaces";

type SessionInfo = {
  session_id: string;
  ws_token: string;
};

type EventsInfo = {
  ws_token: string;
};

const termEl = requireElement("term");
const splash = requireElement("splash");
const approvalRoot = requireElement("approval-overlay");
const toolbar = requireElement("toolbar");
const sessionListRoot = requireElement("session-list");
const sessionsRoot = requireElement("sessions");
const snapshotsRoot = requireElement("snapshots");
const timelineRoot = requireElement("timeline");
const softkeys = requireElement("softkeys");
const toast = requireElement("toast");
let theme = loadTheme();
applyDocumentTheme(theme);
const { term, fit } = createTerminal(termEl, theme);
const ws = new TerminalWS();
const events = new EventsWS();
const approvals = new ApprovalOverlay(approvalRoot);
const anomalies = new AnomalyOverlay(approvalRoot);
let relayE2E: RelayE2E | undefined;
let sessionList: SessionsListView | undefined;
let sessions: SessionsPanel | undefined;
let snapshots: SnapshotsPanel | undefined;
let timeline: TimelinePanel | undefined;
let workspaceSwitcher: WorkspaceSwitcher | undefined;

attachTerminalIO(term, ws);
installViewportResize(term, fit, ws);
installTouchScroll(term, termEl);
installViewportPinning(termEl);
registerServiceWorker();
ws.addEventListener("data", (event) => {
  const data = (event as CustomEvent<ArrayBuffer>).detail;
  term.write(new Uint8Array(data));
});
ws.addEventListener("open", () => {
  splash.hidden = true;
  hideToast();
  fit.fit();
  ws.sendResize(term.rows, term.cols);
});
ws.addEventListener("reconnecting", () => showToast("Reconnecting..."));
events.addEventListener("event", (event) => {
  const envelope = (event as CustomEvent<EventEnvelope>).detail;
  approvals.handleEnvelope(envelope);
  anomalies.handleEnvelope(envelope);
  sessionList?.handleEnvelope(envelope);
  sessions?.handleEnvelope(envelope);
  snapshots?.handleEnvelope(envelope);
  timeline?.handleEnvelope(envelope);
});
events.addEventListener("toast", (event) => {
  const payload = ((event as CustomEvent<EventEnvelope<ToastPayload>>).detail).payload;
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
    anomalies.setPostJSON(postJSON);
    const routeSession = routeSessionID();
    if (routeSession === null) {
      await showSessionsHome();
      return;
    }
    const info = await sessionInfo(routeSession);
    await relayE2E?.bindSession(info.ws_token);
    saveLastSessionID(info.session_id);
    showTerminalChrome();
    sessions = new SessionsPanel(sessionsRoot, info.session_id, getJSON);
    snapshots = new SnapshotsPanel(snapshotsRoot, info.session_id, getJSON, postJSON, navigateToSession, showToast);
    timeline = new TimelinePanel(timelineRoot, info.session_id);
    installControls(toolbar, info, snapshots, timeline);
    new SoftKeyBar({
      root: softkeys,
      sendBytes: (data) => ws.sendBinary(data),
      sendText: (data) => ws.sendText(data),
      pageUp: () => postControl(info.session_id, "page_up"),
      pageDown: () => postControl(info.session_id, "page_down"),
      focus: () => term.focus(),
      getTheme: () => theme,
      setTheme: setTheme
    });
    connectTerminal(info);
    events.connect(eventsURL(info.ws_token));
  } catch {
    splash.textContent = "session unavailable";
  }
}

async function showSessionsHome(): Promise<void> {
  showListChrome();
  const workspaceControl = document.createElement("div");
  const list = new SessionsListView(sessionListRoot, getJSON, navigateToSession, workspaceControl);
  sessionList = list;
  workspaceSwitcher = new WorkspaceSwitcher(workspaceControl, getJSON, postJSON, (name) => {
    list.setWorkspace(name);
    setRouteWorkspace(name);
  }, showToast);
  await workspaceSwitcher.load();
  list.setWorkspace(workspaceSwitcher.current(), false);
  await list.load();
  await connectSessionListEvents();
  splash.hidden = true;
}

async function sessionInfo(sessionID: string): Promise<SessionInfo> {
  return getJSON<SessionInfo>(`/session-info?session_id=${encodeURIComponent(sessionID)}`);
}

async function connectSessionListEvents(): Promise<void> {
  try {
    const info = await getJSON<EventsInfo>("/session-info?events=1");
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

function setRouteWorkspace(name: string): void {
  const url = new URL(window.location.href);
  if (name.trim() === "") {
    url.searchParams.delete("workspace");
  } else {
    url.searchParams.set("workspace", name.trim());
  }
  window.history.replaceState(null, "", url);
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

function installControls(root: HTMLElement, info: SessionInfo, snapshotsPanel: SnapshotsPanel, timelinePanel: TimelinePanel): void {
  root.replaceChildren(
    controlButton("TL", () => timelinePanel.toggle()),
    controlButton("SNAP", () => snapshotsPanel.toggle()),
    controlButton("MAC", () => postHandover(info, "mac")),
    controlButton("PHONE", () => postHandover(info, "phone")),
    controlButton("INT", () => postControl(info.session_id, "interrupt")),
    controlButton("KILL", () => postControl(info.session_id, "kill"))
  );
}

function controlButton(label: string, action: () => void): HTMLButtonElement {
  const el = document.createElement("button");
  let firedAt = 0;
  const fire = (event: Event) => {
    event.preventDefault();
    if (Date.now() - firedAt < 250) {
      return;
    }
    firedAt = Date.now();
    action();
    term.focus();
  };
  el.type = "button";
  el.className = "control-button";
  el.textContent = label;
  el.tabIndex = -1;
  el.addEventListener("pointerdown", fire);
  el.addEventListener("touchstart", fire, { passive: false });
  el.addEventListener("click", (event) => {
    event.preventDefault();
    if (Date.now() - firedAt > 500) {
      action();
      term.focus();
    }
  });
  return el;
}

function postControl(sessionID: string, action: string): void {
  void postJSON("/control", { session_id: sessionID, action });
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

function postHandover(info: SessionInfo, target: "mac" | "phone"): void {
  void (async () => {
    showToast(target === "mac" ? "Opening on Mac..." : "Returning to phone...");
    ws.close();
    try {
      const response = await postJSON("/handover", { session_id: info.session_id, target });
      const text = await response.text();
      let body = {} as { message?: unknown };
      try {
        body = text === "" ? {} : (JSON.parse(text) as { message?: unknown });
      } catch {
        body = {};
      }
      if (!response.ok) {
        const msg = typeof body.message === "string" ? body.message : text.trim() || `handover ${response.status}`;
        throw new Error(msg);
      }
      const msg = typeof body.message === "string" && body.message.trim() !== "" ? body.message : "Handover complete.";
      showToast(msg);
      if (target === "phone") {
        connectTerminal(info);
      }
    } catch (err) {
      connectTerminal(info);
      showToast(err instanceof Error ? err.message : "Handover failed.");
    } finally {
      term.focus();
    }
  })();
}

async function postJSON(path: string, body: Record<string, unknown>): Promise<Response> {
  const raw = JSON.stringify(body);
  return fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": relayE2E === undefined ? "application/json" : "application/vnd.onibi.e2e+json" },
    body: relayE2E === undefined ? raw : await relayE2E.sealText(raw, `http:POST:${path}`)
  });
}

function setTheme(next: TerminalThemeName): void {
  theme = next;
  window.localStorage.setItem("onibi-theme", next);
  applyDocumentTheme(next);
  applyTerminalTheme(term, next);
}

function loadTheme(): TerminalThemeName {
  return window.localStorage.getItem("onibi-theme") === "light" ? "light" : "dark";
}

function applyDocumentTheme(next: TerminalThemeName): void {
  document.documentElement.dataset.theme = next;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", next === "dark" ? "#090b0f" : "#f6f8fa");
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
  sessionsRoot.hidden = true;
  snapshotsRoot.hidden = true;
  timelineRoot.hidden = true;
  termEl.hidden = true;
  softkeys.hidden = true;
  approvalRoot.hidden = true;
  sessionListRoot.hidden = false;
}

function showTerminalChrome(): void {
  toolbar.hidden = false;
  termEl.hidden = false;
  softkeys.hidden = false;
  approvalRoot.hidden = false;
  sessionListRoot.hidden = true;
}

function installViewportPinning(root: HTMLElement): void {
  const viewport = window.visualViewport;
  if (viewport == null) {
    return;
  }
  let frame = 0;
  const pin = () => {
    window.cancelAnimationFrame(frame);
    frame = window.requestAnimationFrame(() => {
      document.documentElement.style.setProperty("--visual-viewport-height", `${viewport.height}px`);
      const keyboardBottom = Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop);
      document.documentElement.style.setProperty("--keyboard-bottom", `${keyboardBottom}px`);
      const cursor = root.querySelector<HTMLElement>(".xterm-helper-textarea");
      cursor?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
  };
  viewport.addEventListener("resize", pin);
  viewport.addEventListener("scroll", pin);
  pin();
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
