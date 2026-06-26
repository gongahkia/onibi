import { TerminalWS } from "./ws";
import { applyTerminalTheme, attachTerminalIO, createTerminal, installViewportResize } from "./terminal";
import type { TerminalThemeName } from "./terminal";
import { ApprovalOverlay } from "./approval";
import { EventsWS } from "./events";
import { SoftKeyBar } from "./softkeys";

type SessionInfo = {
  session_id: string;
  ws_token: string;
};

const termEl = requireElement("term");
const splash = requireElement("splash");
const approvalRoot = requireElement("approval-overlay");
const toolbar = requireElement("toolbar");
const softkeys = requireElement("softkeys");
const toast = requireElement("toast");
let theme = loadTheme();
applyDocumentTheme(theme);
const { term, fit } = createTerminal(termEl, theme);
const ws = new TerminalWS();
const events = new EventsWS();
const approvals = new ApprovalOverlay(approvalRoot);

attachTerminalIO(term, ws);
installViewportResize(term, fit, ws);
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
events.addEventListener("event", (event) => approvals.handleEnvelope((event as CustomEvent).detail));

void boot();

async function boot(): Promise<void> {
  try {
    const info = await sessionInfo();
    installControls(toolbar, info);
    new SoftKeyBar({
      root: softkeys,
      sendBytes: (data) => ws.sendBinary(data),
      sendText: (data) => ws.sendText(data),
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

async function sessionInfo(): Promise<SessionInfo> {
  const params = new URLSearchParams(window.location.search);
  const sessionID = params.get("session_id");
  const path = sessionID === null ? "/session-info" : `/session-info?session_id=${encodeURIComponent(sessionID)}`;
  const response = await fetch(path, { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error(`session-info ${response.status}`);
  }
  return (await response.json()) as SessionInfo;
}

function wsURL(token: string): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/ws/pty?token=${encodeURIComponent(token)}`;
}

function eventsURL(token: string): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/ws/events?token=${encodeURIComponent(token)}`;
}

function installControls(root: HTMLElement, info: SessionInfo): void {
  root.replaceChildren(
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
  void fetch("/control", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionID, action })
  });
}

function connectTerminal(info: SessionInfo): void {
  ws.connect(wsURL(info.ws_token), info.session_id, 0);
}

function postHandover(info: SessionInfo, target: "mac" | "phone"): void {
  void (async () => {
    showToast(target === "mac" ? "Opening on Mac..." : "Returning to phone...");
    ws.close();
    try {
      const response = await fetch("/handover", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session_id: info.session_id, target })
      });
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
