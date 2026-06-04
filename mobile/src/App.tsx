import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, FormEvent, SetStateAction } from "react";

const STORAGE_KEY = "onibi.mobile.connection";
const DEVICE_LABEL = "Onibi Mobile PWA";

type Decision = "allow" | "deny";
type WsState = "idle" | "connecting" | "open" | "closed";
type Tab = "pending" | "recent" | "terminal";

export interface TransportEndpoint {
  name: string;
  url: string;
  fingerprint?: string | null;
}

export interface PairingPayload {
  protocol_version?: string;
  machine_id?: string;
  machineId?: string;
  host?: string;
  port?: number;
  token: string;
  vapid_public_key?: string;
  vapidPublicKey?: string;
  transports?: TransportEndpoint[];
}

interface PairResponse {
  deviceId: string;
  machineId: string;
}

export interface Connection {
  baseUrl: string;
  token: string;
  deviceId: string;
  machineId: string;
  vapidPublicKey?: string;
  transports: TransportEndpoint[];
}

export interface Approval {
  approval_id: string;
  machine_id: string;
  session_id: string;
  agent: string;
  tool: string;
  input: unknown;
  cwd: string;
  metadata?: unknown;
  created_at: number;
}

interface RunEvent {
  id?: number;
  session_id: string;
  kind: string;
  payload: unknown;
  ts: number;
}

interface ServerMessage {
  type: string;
  approval_id?: string;
  machine_id?: string;
  session_id?: string;
  agent?: string;
  tool?: string;
  input?: unknown;
  cwd?: string;
  metadata?: unknown;
  decision?: Decision;
  kind?: string;
  payload?: unknown;
  data?: string;
}

type BarcodeDetectorCtor = new (options: { formats: string[] }) => {
  detect(image: ImageBitmapSource): Promise<Array<{ rawValue: string }>>;
};

function App() {
  const [connection, setConnection] = useStoredConnection();

  return connection ? (
    <InboxView connection={connection} onForget={() => setConnection(null)} />
  ) : (
    <PairingView onPaired={setConnection} />
  );
}

function useStoredConnection(): [Connection | null, (next: Connection | null) => void] {
  const [connection, setConnectionState] = useState<Connection | null>(() => {
    if (!storageAvailable()) {
      return null;
    }
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Connection) : null;
  });

  const setConnection = (next: Connection | null) => {
    setConnectionState(next);
    if (!storageAvailable()) {
      return;
    }
    if (next) {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } else {
      window.localStorage.removeItem(STORAGE_KEY);
    }
  };

  return [connection, setConnection];
}

function PairingView({ onPaired }: { onPaired: (connection: Connection) => void }) {
  const [raw, setRaw] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("Paste the pairing payload from Onibi.");

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    setMessage("Pairing...");
    try {
      const payload = parsePairingInput(raw);
      const baseUrl = chooseBaseUrl(payload);
      const vapidPublicKey = payload.vapid_public_key ?? payload.vapidPublicKey;
      const pushSubscription = await createPushSubscription(vapidPublicKey);
      const response = await authedFetch(baseUrl, payload.token, "/v1/pair", {
        method: "POST",
        body: JSON.stringify({
          deviceLabel: DEVICE_LABEL,
          pushSubscription,
        }),
      });
      const paired = (await response.json()) as PairResponse;
      onPaired({
        baseUrl,
        token: payload.token,
        deviceId: paired.deviceId,
        machineId: paired.machineId,
        vapidPublicKey,
        transports: payload.transports ?? [],
      });
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };

  const scanFile = async (file: File | undefined) => {
    if (!file) {
      return;
    }
    setBusy(true);
    setMessage("Scanning QR image...");
    try {
      setRaw(await scanQrImage(file));
      setMessage("QR payload scanned.");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="pairing-screen">
      <section className="pairing-panel" aria-labelledby="pair-title">
        <div className="brand-row">
          <img src="/favicon.svg" alt="" className="brand-mark" />
          <div>
            <p className="eyebrow">Onibi</p>
            <h1 id="pair-title">Pair this device</h1>
          </div>
        </div>

        <form onSubmit={submit} className="pair-form">
          <textarea
            value={raw}
            onChange={(event) => setRaw(event.target.value)}
            placeholder='{"token":"...","transports":[{"url":"https://..."}]}'
            rows={8}
            aria-label="Pairing payload"
          />
          <div className="pair-actions">
            <label className="secondary-button">
              Scan QR image
              <input
                type="file"
                accept="image/*"
                onChange={(event) => void scanFile(event.target.files?.[0])}
              />
            </label>
            <button type="submit" disabled={busy || raw.trim().length === 0}>
              Pair
            </button>
          </div>
        </form>
        <p className="status-line">{message}</p>
        <OnboardingView />
      </section>
    </main>
  );
}

function InboxView({
  connection,
  onForget,
}: {
  connection: Connection;
  onForget: () => void;
}) {
  const [pending, setPending] = useState<Approval[]>([]);
  const [recent, setRecent] = useState<RunEvent[]>([]);
  const [terminalOutput, setTerminalOutput] = useState<Record<string, string[]>>({});
  const [selectedSession, setSelectedSession] = useState<string>("");
  const [tab, setTab] = useState<Tab>("pending");
  const [wsState, setWsState] = useState<WsState>("idle");
  const [error, setError] = useState<string | null>(null);

  const loadInitial = useCallback(async () => {
    const [pendingResponse, recentResponse] = await Promise.all([
      authedFetch(connection.baseUrl, connection.token, "/v1/approval/pending"),
      authedFetch(connection.baseUrl, connection.token, "/v1/run/recent"),
    ]);
    setPending((await pendingResponse.json()) as Approval[]);
    setRecent((await recentResponse.json()) as RunEvent[]);
  }, [connection.baseUrl, connection.token]);

  useEffect(() => {
    void loadInitial().catch((err) =>
      setError(err instanceof Error ? err.message : String(err)),
    );
  }, [loadInitial]);

  useEffect(() => {
    let closed = false;
    let socket: WebSocket | undefined;
    let reconnectTimer = 0;
    let attempt = 0;

    const connect = () => {
      setWsState("connecting");
      socket = new WebSocket(realtimeUrl(connection));
      socket.onopen = () => {
        attempt = 0;
        setWsState("open");
        setError(null);
      };
      socket.onmessage = (event) => {
        const message = JSON.parse(String(event.data)) as ServerMessage;
        if (message.type === "ping" && socket?.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "pong" }));
        }
        applyServerMessage(message, setPending, setRecent, setTerminalOutput, setSelectedSession);
      };
      socket.onerror = () => setError("Realtime connection failed.");
      socket.onclose = () => {
        setWsState("closed");
        if (closed) {
          return;
        }
        reconnectTimer = window.setTimeout(connect, reconnectDelay(attempt));
        attempt += 1;
      };
    };

    connect();
    return () => {
      closed = true;
      window.clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, [connection]);

  const decide = async (
    approval: Approval,
    decision: Decision,
    editedCommand?: string,
  ) => {
    await authedFetch(connection.baseUrl, connection.token, `/v1/approval/${approval.approval_id}/decide`, {
      method: "POST",
      body: JSON.stringify(buildDecisionBody(approval, decision, editedCommand)),
    });
    setPending((items) => items.filter((item) => item.approval_id !== approval.approval_id));
  };

  const sessionOptions = useMemo(
    () => Object.keys(terminalOutput).sort(),
    [terminalOutput],
  );
  const activeSession = selectedSession || sessionOptions[0] || "";

  return (
    <main className="app-shell">
      <header className="top-bar">
        <div>
          <p className="eyebrow">{connection.machineId}</p>
          <h1>Approvals</h1>
        </div>
        <div className="top-actions">
          <span className={`ws-pill ${wsState}`}>{wsState}</span>
          <button type="button" className="ghost-button" onClick={onForget}>
            Unpair
          </button>
        </div>
      </header>

      <nav className="tab-strip" aria-label="Mobile views">
        <button className={tab === "pending" ? "active" : ""} onClick={() => setTab("pending")}>
          Pending <span>{pending.length}</span>
        </button>
        <button className={tab === "recent" ? "active" : ""} onClick={() => setTab("recent")}>
          Recent <span>{recent.length}</span>
        </button>
        <button className={tab === "terminal" ? "active" : ""} onClick={() => setTab("terminal")}>
          Terminal <span>{sessionOptions.length}</span>
        </button>
      </nav>

      {error ? <p className="error-line">{error}</p> : null}

      {tab === "pending" ? (
        <section className="approval-list">
          {pending.length === 0 ? (
            <EmptyState title="No pending approvals" body="New agent tool calls appear here." />
          ) : (
            pending.map((approval) => (
              <ApprovalCard key={approval.approval_id} approval={approval} onDecide={decide} />
            ))
          )}
        </section>
      ) : null}

      {tab === "recent" ? (
        <section className="run-list">
          {recent.length === 0 ? (
            <EmptyState title="No recent runs" body="Agent run events appear after pairing." />
          ) : (
            recent.map((event) => (
              <button
                key={`${event.id ?? event.ts}-${event.session_id}-${event.kind}`}
                className="run-row"
                type="button"
                onClick={() => {
                  setSelectedSession(event.session_id);
                  setTab("terminal");
                }}
              >
                <span>{event.kind}</span>
                <strong>{event.session_id}</strong>
                <time>{formatTime(event.ts)}</time>
              </button>
            ))
          )}
        </section>
      ) : null}

      {tab === "terminal" ? (
        <TerminalMirrorView
          sessionId={activeSession}
          sessions={sessionOptions}
          chunks={terminalOutput[activeSession] ?? []}
          onSelect={setSelectedSession}
        />
      ) : null}
    </main>
  );
}

function ApprovalCard({
  approval,
  onDecide,
}: {
  approval: Approval;
  onDecide: (approval: Approval, decision: Decision, editedCommand?: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [command, setCommand] = useState(commandText(approval.input));
  const [busy, setBusy] = useState(false);

  const submit = async (decision: Decision, editedCommand?: string) => {
    setBusy(true);
    try {
      await onDecide(approval, decision, editedCommand);
    } finally {
      setBusy(false);
    }
  };

  return (
    <article className="approval-card">
      <div className="approval-header">
        <div>
          <p className="eyebrow">{approval.agent}</p>
          <h2>{approval.tool}</h2>
        </div>
        <time>{formatTime(approval.created_at)}</time>
      </div>
      <p className="cwd">{approval.cwd}</p>
      {editing ? (
        <textarea
          value={command}
          onChange={(event) => setCommand(event.target.value)}
          rows={5}
          aria-label="Edited command"
        />
      ) : (
        <pre>{command}</pre>
      )}
      <div className="approval-actions">
        <button type="button" disabled={busy} onClick={() => void submit("allow")}>
          Allow
        </button>
        {editing ? (
          <button type="button" disabled={busy} onClick={() => void submit("allow", command)}>
            Approve Edit
          </button>
        ) : (
          <button type="button" disabled={busy} onClick={() => setEditing(true)}>
            Edit
          </button>
        )}
        <button
          type="button"
          className="danger-button"
          disabled={busy}
          onClick={() => void submit("deny")}
        >
          Deny
        </button>
      </div>
    </article>
  );
}

function TerminalMirrorView({
  sessionId,
  sessions,
  chunks,
  onSelect,
}: {
  sessionId: string;
  sessions: string[];
  chunks: string[];
  onSelect: (sessionId: string) => void;
}) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<{ write(data: string): void; dispose(): void } | null>(null);
  const fitRef = useRef<{ fit(): void; dispose(): void } | null>(null);
  const writtenRef = useRef(0);
  const plainText = chunks.map(decodeBase64Text).join("");

  useEffect(() => {
    const container = terminalRef.current;
    if (!container || !sessionId) {
      return undefined;
    }
    let disposed = false;
    writtenRef.current = 0;
    void Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]).then(
      ([xterm, fit]) => {
        if (disposed) {
          return;
        }
        const term = new xterm.Terminal({
          convertEol: true,
          disableStdin: true,
          fontFamily: "Menlo, Monaco, monospace",
          fontSize: 12,
          scrollback: 4000,
          theme: { background: "#080b10", foreground: "#dbe5ed" },
        });
        const fitAddon = new fit.FitAddon();
        term.loadAddon(fitAddon);
        term.open(container);
        fitAddon.fit();
        termRef.current = term;
        fitRef.current = fitAddon;
      },
    );
    return () => {
      disposed = true;
      fitRef.current?.dispose();
      termRef.current?.dispose();
      fitRef.current = null;
      termRef.current = null;
    };
  }, [sessionId]);

  useEffect(() => {
    const term = termRef.current;
    if (!term) {
      return;
    }
    for (let index = writtenRef.current; index < chunks.length; index += 1) {
      term.write(decodeBase64Text(chunks[index]));
    }
    writtenRef.current = chunks.length;
    fitRef.current?.fit();
  }, [chunks]);

  return (
    <section className="terminal-pane">
      <div className="terminal-toolbar">
        <select
          value={sessionId}
          onChange={(event) => onSelect(event.target.value)}
          aria-label="Terminal session"
        >
          {sessions.length === 0 ? <option value="">No sessions</option> : null}
          {sessions.map((session) => (
            <option key={session} value={session}>
              {session}
            </option>
          ))}
        </select>
      </div>
      <div ref={terminalRef} className="terminal-mount" />
      <pre className="terminal-fallback" aria-label="Terminal mirror fallback">
        {plainText || "Waiting for terminal output..."}
      </pre>
    </section>
  );
}

function OnboardingView() {
  const ios = typeof navigator !== "undefined" && /iPad|iPhone|iPod/.test(navigator.userAgent);
  return (
    <div className="onboarding">
      <strong>{ios ? "iOS home-screen install required for push." : "Push-ready PWA."}</strong>
      <span>
        {ios
          ? "Open this page from Safari, share it, then add it to Home Screen."
          : "Install from the browser menu for lock-screen notifications."}
      </span>
    </div>
  );
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="empty-state">
      <h2>{title}</h2>
      <p>{body}</p>
    </div>
  );
}

function applyServerMessage(
  message: ServerMessage,
  setPending: Dispatch<SetStateAction<Approval[]>>,
  setRecent: Dispatch<SetStateAction<RunEvent[]>>,
  setTerminalOutput: Dispatch<SetStateAction<Record<string, string[]>>>,
  setSelectedSession: Dispatch<SetStateAction<string>>,
) {
  if (message.type === "approval-pending" && message.approval_id && message.session_id) {
    const approval: Approval = {
      approval_id: message.approval_id,
      machine_id: message.machine_id ?? "",
      session_id: message.session_id,
      agent: message.agent ?? "agent",
      tool: message.tool ?? "tool",
      input: message.input ?? {},
      cwd: message.cwd ?? "",
      metadata: message.metadata,
      created_at: Date.now(),
    };
    setPending((items) => [
      approval,
      ...items.filter((item) => item.approval_id !== approval.approval_id),
    ]);
  }
  if (message.type === "approval-resolved" && message.approval_id) {
    setPending((items) => items.filter((item) => item.approval_id !== message.approval_id));
  }
  if (message.type === "run-event" && message.session_id && message.kind) {
    setRecent((items) => [
      {
        session_id: message.session_id ?? "",
        kind: message.kind ?? "event",
        payload: message.payload ?? {},
        ts: Date.now(),
      },
      ...items,
    ].slice(0, 50));
  }
  if (message.type === "pty-output" && message.session_id && message.data) {
    setSelectedSession((current) => current || message.session_id || "");
    setTerminalOutput((items) => ({
      ...items,
      [message.session_id ?? ""]: [...(items[message.session_id ?? ""] ?? []), message.data ?? ""],
    }));
  }
}

export function parsePairingInput(raw: string): PairingPayload {
  const trimmed = raw.trim();
  if (!trimmed) {
    throw new Error("Pairing payload is empty.");
  }
  if (trimmed.startsWith("onibi://")) {
    const url = new URL(trimmed);
    const payload = url.searchParams.get("payload") ?? url.searchParams.get("data");
    if (payload) {
      return parsePairingInput(decodeURIComponent(payload));
    }
    const token = url.searchParams.get("token");
    const baseUrl = url.searchParams.get("baseUrl") ?? url.searchParams.get("url");
    if (token && baseUrl) {
      return {
        token,
        machineId: url.searchParams.get("machineId") ?? undefined,
        vapidPublicKey: url.searchParams.get("vapidPublicKey") ?? undefined,
        transports: [{ name: "deep-link", url: baseUrl }],
      };
    }
  }
  try {
    const parsed = JSON.parse(trimmed) as PairingPayload;
    if (!parsed.token) {
      throw new Error("Pairing payload is missing token.");
    }
    return parsed;
  } catch (jsonError) {
    try {
      return parsePairingInput(atob(trimmed));
    } catch {
      throw jsonError instanceof Error ? jsonError : new Error("Invalid pairing payload.");
    }
  }
}

export function chooseBaseUrl(payload: PairingPayload): string {
  const transport = payload.transports?.find((item) => !item.url.includes("127.0.0.1"))
    ?? payload.transports?.[0];
  if (transport?.url) {
    return normalizeBaseUrl(transport.url);
  }
  if (payload.host && payload.port) {
    return normalizeBaseUrl(`http://${payload.host}:${payload.port}`);
  }
  throw new Error("Pairing payload has no reachable transport.");
}

export function reconnectDelay(attempt: number): number {
  return Math.min(30_000, 1_000 * 2 ** Math.max(0, attempt));
}

export function buildDecisionBody(
  approval: Approval,
  decision: Decision,
  editedCommand?: string,
) {
  const body: {
    decision: Decision;
    by: string;
    reason?: string;
    updatedInput?: unknown;
  } = { decision, by: "mobile" };
  if (decision === "deny") {
    body.reason = "denied from mobile";
  }
  if (decision === "allow" && editedCommand !== undefined) {
    body.updatedInput = editedInput(approval.input, editedCommand);
    body.reason = "edited from mobile";
  }
  return body;
}

export function commandText(input: unknown): string {
  if (isRecord(input) && typeof input.command === "string") {
    return input.command;
  }
  return JSON.stringify(input, null, 2);
}

function editedInput(input: unknown, editedCommand: string): unknown {
  if (isRecord(input) && typeof input.command === "string") {
    return { ...input, command: editedCommand };
  }
  try {
    return JSON.parse(editedCommand);
  } catch {
    return editedCommand;
  }
}

async function createPushSubscription(vapidPublicKey: string | undefined): Promise<unknown | null> {
  if (
    !vapidPublicKey ||
    typeof window === "undefined" ||
    !("serviceWorker" in navigator) ||
    !("PushManager" in window) ||
    !("Notification" in window)
  ) {
    return null;
  }
  if (Notification.permission === "default") {
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
      return null;
    }
  }
  if (Notification.permission !== "granted") {
    return null;
  }
  const registration = await navigator.serviceWorker.ready;
  const existing = await registration.pushManager.getSubscription();
  const subscription =
    existing ??
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(vapidPublicKey),
    }));
  return subscription.toJSON();
}

async function scanQrImage(file: File): Promise<string> {
  if (typeof window === "undefined" || !("BarcodeDetector" in window)) {
    throw new Error("QR image scanning is not available in this browser.");
  }
  const detector = new ((window as unknown as { BarcodeDetector: BarcodeDetectorCtor })
    .BarcodeDetector)({ formats: ["qr_code"] });
  const bitmap = await createImageBitmap(file);
  const codes = await detector.detect(bitmap);
  const raw = codes[0]?.rawValue;
  if (!raw) {
    throw new Error("No QR payload found in image.");
  }
  return raw;
}

function authedFetch(
  baseUrl: string,
  token: string,
  path: string,
  init: RequestInit = {},
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("authorization", `Bearer ${token}`);
  if (init.body && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }
  return fetch(`${normalizeBaseUrl(baseUrl)}${path}`, { ...init, headers }).then((response) => {
    if (!response.ok) {
      throw new Error(`${path} failed with HTTP ${response.status}`);
    }
    return response;
  });
}

function realtimeUrl(connection: Connection): string {
  const base = new URL(normalizeBaseUrl(connection.baseUrl));
  base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
  base.pathname = "/v1/realtime";
  base.search = `token=${encodeURIComponent(connection.token)}`;
  return base.toString();
}

function normalizeBaseUrl(url: string): string {
  return url.replace(/\/+$/, "");
}

function decodeBase64Text(data: string): string {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new TextDecoder().decode(bytes);
}

function urlBase64ToUint8Array(value: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (value.length % 4)) % 4);
  const base64 = `${value}${padding}`.replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const output = new Uint8Array(new ArrayBuffer(raw.length));
  for (let index = 0; index < raw.length; index += 1) {
    output[index] = raw.charCodeAt(index);
  }
  return output;
}

function formatTime(value: number): string {
  const date = value > 10_000_000_000 ? new Date(value) : new Date(value * 1000);
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function storageAvailable(): boolean {
  return typeof window !== "undefined" && "localStorage" in window;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export default App;
