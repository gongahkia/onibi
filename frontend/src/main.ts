import { TerminalWS } from "./ws";
import { attachTerminalIO, createTerminal, installViewportResize } from "./terminal";
import { ApprovalOverlay } from "./approval";
import { EventsWS } from "./events";

type SessionInfo = {
  session_id: string;
  ws_token: string;
};

const termEl = requireElement("term");
const splash = requireElement("splash");
const approvalRoot = requireElement("approval-overlay");
const toolbar = requireElement("toolbar");
const { term, fit } = createTerminal(termEl);
const ws = new TerminalWS();
const events = new EventsWS();
const approvals = new ApprovalOverlay(approvalRoot);

attachTerminalIO(term, ws);
installViewportResize(term, fit, ws);
ws.addEventListener("data", (event) => {
  const data = (event as CustomEvent<ArrayBuffer>).detail;
  term.write(new Uint8Array(data));
});
ws.addEventListener("open", () => {
  splash.hidden = true;
  fit.fit();
  ws.sendResize(term.rows, term.cols);
});
events.addEventListener("event", (event) => approvals.handleEnvelope((event as CustomEvent).detail));

void boot();

async function boot(): Promise<void> {
  try {
    const info = await sessionInfo();
    installControls(toolbar, info.session_id);
    ws.connect(wsURL(info.ws_token), info.session_id, 0);
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

function installControls(root: HTMLElement, sessionID: string): void {
  root.replaceChildren(controlButton("INT", () => postControl(sessionID, "interrupt")), controlButton("KILL", () => postControl(sessionID, "kill")));
}

function controlButton(label: string, action: () => void): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "control-button";
  el.textContent = label;
  el.addEventListener("click", action);
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

function requireElement(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`missing #${id}`);
  }
  return el;
}
