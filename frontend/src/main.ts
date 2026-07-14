import "./main.css";
import { TerminalWS } from "./ws";
import {
  applyTerminalTheme,
  attachTerminalIO,
  clampTerminalFontSize,
  createTerminal,
  defaultTerminalFontSize,
  defaultTerminalTheme,
  installTouchScroll,
  installViewportResize,
  isTerminalThemeName,
  logCrampedPortraitCols,
  terminalFontSizeKey
} from "./terminal";
import type { TerminalThemeName } from "./terminal";
import { AnomalyOverlay } from "./anomaly";
import { ApprovalOverlay } from "./approval";
import { EventsWS } from "./events";
import type { EventEnvelope, ToastPayload } from "./events";
import { SoftKeyBar } from "./softkeys";
import { RelayE2E, relayE2EContentType } from "./e2e";
import { saveLastSessionID, SessionsListView, SessionsPanel } from "./sessions";
import { SnapshotsPanel } from "./snapshots";
import { TimelinePanel } from "./timeline";
import { RecordingPlayerPanel } from "./recording-player";
import { FilesPanel } from "./files";
import { refreshPushSubscription, subscribePushFromGesture } from "./push";
import { startFirstRunTour } from "./tour";
import { ApprovalWakeLock } from "./wake-lock";
import { installImagePaste } from "./image-paste";
import type { ImageUploadRequest } from "./image-paste";
import { SharePanel } from "./share";
import { AgentsFeed } from "./agents-feed";
import { PairedHostsPanel } from "./paired-hosts-panel";
import { FleetHostsPanel } from "./fleet-hosts";
import { ApprovalInboxPanel } from "./approval-inbox";
import { FleetHomeView } from "./fleet-home";
import { SessionPickerPanel } from "./session-picker";
import { TerminalStatus } from "./terminal-status";
import { InterventionPanel } from "./intervention-panel";

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
const sessionsRoot = requireElement("sessions");
const snapshotsRoot = requireElement("snapshots");
const timelineRoot = requireElement("timeline");
const recordingsRoot = requireElement("recordings");
const filesRoot = requireElement("files");
const softkeys = requireElement("softkeys");
const toast = requireElement("toast");
let theme = loadTheme();
applyDocumentTheme(theme);
const { term, fit } = createTerminal(termEl, theme);
const ws = new TerminalWS();
const events = new EventsWS();
const approvalWakeLock = new ApprovalWakeLock();
const approvals = new ApprovalOverlay(approvalRoot, approvalWakeLock);
const anomalies = new AnomalyOverlay(approvalRoot);
let relayE2E: RelayE2E | undefined;
let sessionList: SessionsListView | undefined;
let sessions: SessionsPanel | undefined;
let snapshots: SnapshotsPanel | undefined;
let timeline: TimelinePanel | undefined;
let recordings: RecordingPlayerPanel | undefined;
let sharePanel: SharePanel | undefined;
let agentsFeed: AgentsFeed | undefined;
let pairedHosts: PairedHostsPanel | undefined;
let fleetHosts: FleetHostsPanel | undefined;
let approvalInbox: ApprovalInboxPanel | undefined;
let fleetHome: FleetHomeView | undefined;
let sessionPicker: SessionPickerPanel | undefined;
let interventionPanel: InterventionPanel | undefined;
let terminalInputEnabled = false;
let viewerMode = false;
let csrfToken = "";
const terminalStatus = new TerminalStatus(() => {
  ws.close();
  window.location.href = "/";
});

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
    anomalies.handleEnvelope(envelope);
  }
  sessionList?.handleEnvelope(envelope);
  fleetHome?.handleEnvelope(envelope);
  approvalInbox?.handleEnvelope(envelope);
  sessions?.handleEnvelope(envelope);
  agentsFeed?.handleEnvelope(envelope);
  snapshots?.handleEnvelope(envelope);
  timeline?.handleEnvelope(envelope);
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
    anomalies.setPostJSON(postJSON);
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
    sessions = new SessionsPanel(sessionsRoot, info.session_id, getJSON);
    snapshots = new SnapshotsPanel(
      snapshotsRoot,
      info.session_id,
      getJSON,
      postJSON,
      navigateToSession,
      showToast
    );
    timeline = new TimelinePanel(timelineRoot, info.session_id);
    recordings = new RecordingPlayerPanel(recordingsRoot, getJSON, getText, showToast);
    const filesPanel = new FilesPanel(
      filesRoot,
      info.session_id,
      getJSON,
      putJSON,
      () => theme,
      showToast,
      viewerMode
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
    agentsFeed = viewerMode ? undefined : new AgentsFeed(getJSON, navigateToSession);
    installControls(
      toolbar,
      info,
      agentsFeed,
      snapshots,
      timeline,
      recordings,
      filesPanel,
      sharePanel,
      interventionPanel
    );
    void agentsFeed?.load();
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
  pairedHosts = new PairedHostsPanel(document.body, showToast);
  fleetHosts = new FleetHostsPanel(document.body, getJSON);
  approvalInbox = new ApprovalInboxPanel(document.body, getJSON, postJSON, showToast);
  sessionPicker = new SessionPickerPanel(document.body, getJSON, (session) => {
    if (session.remote_url !== undefined && session.remote_url !== "") {
      window.location.href = session.remote_url;
      return;
    }
    navigateToSession(session.id);
  });
  const hostControls = document.createElement("div");
  hostControls.className = "session-list-header-controls";
  hostControls.append(pairedHosts.element, fleetHosts.element, approvalInbox.element);
  const home = new FleetHomeView(
    sessionListRoot,
    getJSON,
    (session) => {
      if (session.remote_url !== undefined && session.remote_url !== "") {
        window.location.href = session.remote_url;
        return;
      }
      navigateToSession(session.id);
    },
    (hostID) => fleetHosts?.openHost(hostID),
    () => approvalInbox?.open(),
    () => sessionPicker?.open(),
    hostControls
  );
  fleetHome = home;
  const setFleetOffline = (offline: boolean) => {
    home.setOffline(offline);
    approvalInbox?.setOffline(offline);
  };
  window.addEventListener("offline", () => setFleetOffline(true));
  window.addEventListener("online", () => setFleetOffline(false));
  if (!navigator.onLine) {
    setFleetOffline(true);
  }
  await connectSessionListEvents();
  await home.load();
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

async function getText(path: string): Promise<string> {
  const response = await fetch(path, { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error(`${path} ${response.status}`);
  }
  return response.text();
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
  info: SessionInfo,
  agents: AgentsFeed | undefined,
  snapshotsPanel: SnapshotsPanel,
  timelinePanel: TimelinePanel,
  recordingsPanel: RecordingPlayerPanel,
  filesPanel: FilesPanel,
  share: SharePanel | undefined,
  intervention: InterventionPanel | undefined
): void {
  const controls: HTMLElement[] = [
    terminalStatus.element,
    controlButton("TL", () => timelinePanel.toggle()),
    controlButton("SNAP", () => snapshotsPanel.toggle()),
    controlButton("REC", () => recordingsPanel.toggle()),
    controlButton("FILES", () => filesPanel.toggle(), "files"),
    controlButton("PUSH", () => enablePush())
  ];
  if (agents !== undefined) {
    controls.unshift(agents.element);
  }
  if (intervention !== undefined) {
    controls.unshift(intervention.element);
  }
  if (info.role !== "viewer") {
    controls.push(controlButton("SHARE", () => share?.open()));
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

async function putJSON(path: string, body: Record<string, unknown>): Promise<Response> {
  return sendJSON("PUT", path, body);
}

async function uploadImage(request: ImageUploadRequest): Promise<string> {
  const response = await postJSON("/files/upload", request);
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
  window.localStorage.setItem("onibi-theme", next);
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
  const stored = window.localStorage.getItem("onibi-theme");
  return isTerminalThemeName(stored) ? stored : defaultTerminalTheme;
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
  if (next === "dark") {
    return "#090b0f";
  }
  if (next === "catppuccin-mocha") {
    return "#1E1E2E";
  }
  if (next === "tokyo-night") {
    return "#1A1B26";
  }
  if (next === "solarized-dark") {
    return "#002B36";
  }
  return "#282C34";
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
  recordingsRoot.hidden = true;
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
