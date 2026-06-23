import { TerminalWS } from "./ws";
import { attachTerminalIO, createTerminal, installViewportResize } from "./terminal";

type SessionInfo = {
  session_id: string;
  ws_token: string;
};

const termEl = requireElement("term");
const splash = requireElement("splash");
const { term, fit } = createTerminal(termEl);
const ws = new TerminalWS();

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

void boot();

async function boot(): Promise<void> {
  try {
    const info = await sessionInfo();
    ws.connect(wsURL(info.ws_token), info.session_id, 0);
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

function requireElement(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`missing #${id}`);
  }
  return el;
}
