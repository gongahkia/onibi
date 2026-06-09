import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, FormEvent, PointerEvent, SetStateAction } from "react";

const STORAGE_KEY = "onibi.mobile.connection";
const PENDING_CACHE_PREFIX = "onibi.mobile.pending.";
const DEVICE_LABEL = "Onibi Mobile PWA";

type Decision = "allow" | "deny";
type ClientScope = "full" | "read-only";
type TrustMode = "approval-required" | "full-access";
export type WsState = "idle" | "connecting" | "fallback" | "open" | "closed";
type Tab = "pending" | "recent" | "terminal" | "send";
type BeforeInstallPromptEvent = Event & {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed"; platform: string }>;
};

const REMOTE_PRESETS: RemotePreset[] = [
  { key: "continue", label: "Continue", destructive: false },
  { key: "approve", label: "Approve", destructive: false },
  { key: "interrupt", label: "Interrupt", destructive: true },
];

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
  scope?: ClientScope;
  vapid_public_key?: string;
  vapidPublicKey?: string;
  transports?: TransportEndpoint[];
}

interface PairResponse {
  deviceId: string;
  machineId: string;
  scope?: ClientScope;
}

export interface Connection {
  baseUrl: string;
  token: string;
  deviceId: string;
  machineId: string;
  scope: ClientScope;
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

export interface RunEvent {
  id?: number;
  session_id: string;
  kind: string;
  payload: unknown;
  ts: number;
}

export interface PaneTarget {
  paneId: string;
  sessionId: string;
  label: string;
  agent?: string | null;
  workspaceId?: string | null;
  cwd?: string | null;
  status: string;
  trustMode: TrustMode;
}

interface PaneTargetsResponse {
  targets: PaneTarget[];
}

interface PaneSendResponse {
  ok: boolean;
  paneId: string;
  sessionId: string;
  bytes: number;
  auditId: string;
  trustMode: TrustMode;
  destructive: boolean;
  preset?: string | null;
}

interface RemotePreset {
  key: string;
  label: string;
  destructive: boolean;
}

interface PendingRemoteDispatch {
  kind: "text" | "preset";
  preset?: RemotePreset;
  destructive: boolean;
  message: string;
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
  created_at?: number;
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
    return raw ? normalizeConnection(JSON.parse(raw) as Connection) : null;
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

function normalizeConnection(connection: Connection): Connection {
  return {
    ...connection,
    scope: connection.scope ?? "full",
    transports: connection.transports ?? [],
  };
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
        scope: paired.scope ?? payload.scope ?? "full",
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
  const [pending, setPending] = useState<Approval[]>(() => loadCachedPending(connection));
  const [recent, setRecent] = useState<RunEvent[]>([]);
  const [paneTargets, setPaneTargets] = useState<PaneTarget[]>([]);
  const [terminalOutput, setTerminalOutput] = useState<Record<string, string[]>>({});
  const [selectedSession, setSelectedSession] = useState<string>("");
  const [composerTarget, setComposerTarget] = useState<string>("");
  const [tab, setTab] = useState<Tab>("pending");
  const [wsState, setWsState] = useState<WsState>("idle");
  const [error, setError] = useState<string | null>(null);
  const [activeBaseUrl, setActiveBaseUrl] = useState(connection.baseUrl);
  const [killConfirming, setKillConfirming] = useState(false);
  const [killBusy, setKillBusy] = useState(false);
  const [killStatus, setKillStatus] = useState<string | null>(null);
  const [replacePairing, setReplacePairing] = useState(false);
  const [loadedInitial, setLoadedInitial] = useState(false);
  const [targetApprovalId, setTargetApprovalId] = useState(
    () => new URLSearchParams(window.location.search).get("approval"),
  );

  const baseUrlCandidates = useMemo(() => candidateBaseUrls(connection), [connection]);
  const activeTransport = useMemo(
    () => transportLabelForUrl(connection, activeBaseUrl),
    [activeBaseUrl, connection],
  );

  const fetchWithFallback = useCallback(
    async (path: string, init: RequestInit = {}) => {
      let lastError: unknown = null;
      const candidates = uniqueStrings([activeBaseUrl, ...baseUrlCandidates]);
      for (const baseUrl of candidates) {
        try {
          const response = await authedFetch(baseUrl, connection.token, path, init);
          if (baseUrl !== activeBaseUrl) {
            setActiveBaseUrl(baseUrl);
          }
          return response;
        } catch (caught) {
          lastError = caught;
        }
      }
      throw lastError instanceof Error ? lastError : new Error("No paired transport is reachable.");
    },
    [activeBaseUrl, baseUrlCandidates, connection.token],
  );

  const loadInitial = useCallback(async () => {
    const [pendingResponse, recentResponse, targetsResponse] = await Promise.all([
      fetchWithFallback("/v1/approval/pending"),
      fetchWithFallback("/v1/run/recent"),
      fetchWithFallback("/v1/panes/targets"),
    ]);
    setPending((await pendingResponse.json()) as Approval[]);
    setRecent((await recentResponse.json()) as RunEvent[]);
    const targets = (await targetsResponse.json()) as PaneTargetsResponse;
    setPaneTargets(targets.targets ?? []);
    setLoadedInitial(true);
  }, [fetchWithFallback]);

  useEffect(() => {
    cachePending(connection, pending);
  }, [connection, pending]);

  useEffect(() => {
    void loadInitial().catch((err) =>
      setError(err instanceof Error ? err.message : String(err)),
    );
  }, [loadInitial]);

  useEffect(() => {
    const onPopState = () => {
      setTargetApprovalId(new URLSearchParams(window.location.search).get("approval"));
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    let closed = false;
    let socket: WebSocket | undefined;
    let reconnectTimer = 0;
    let attempt = 0;

    const connect = () => {
      setWsState("connecting");
      socket = new WebSocket(realtimeUrl({ ...connection, baseUrl: activeBaseUrl }));
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
        const fallbackUrl = nextBaseUrl(activeBaseUrl, baseUrlCandidates);
        if (fallbackUrl !== activeBaseUrl) {
          setWsState("fallback");
          setActiveBaseUrl(fallbackUrl);
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
  }, [activeBaseUrl, baseUrlCandidates, connection]);

  const decide = async (
    approval: Approval,
    decision: Decision,
    editedCommand?: string,
    reason?: string,
  ) => {
    await fetchWithFallback(`/v1/approval/${approval.approval_id}/decide`, {
      method: "POST",
      body: JSON.stringify(buildDecisionBody(approval, decision, editedCommand, reason)),
    });
    setPending((items) => items.filter((item) => item.approval_id !== approval.approval_id));
    if (targetApprovalId === approval.approval_id) {
      window.history.replaceState({}, "", "/m/");
      setTargetApprovalId(null);
    }
  };

  const emergencyStop = async () => {
    setKillBusy(true);
    setKillStatus(null);
    try {
      const response = await fetchWithFallback("/v1/emergency-stop", emergencyStopRequest());
      const result = (await response.json()) as {
        deniedApprovals?: number;
        stoppedSessions?: number;
      };
      setPending([]);
      setKillConfirming(false);
      setKillStatus(
        `Stopped ${result.stoppedSessions ?? 0} sessions and denied ${result.deniedApprovals ?? 0} approvals.`,
      );
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setKillBusy(false);
    }
  };

  const sessionOptions = useMemo(
    () => Object.keys(terminalOutput).sort(),
    [terminalOutput],
  );
  const targetOptions = useMemo(
    () => buildPaneTargetOptions(paneTargets, recent, terminalOutput, pending),
    [paneTargets, recent, terminalOutput, pending],
  );
  const resolvedComposerTarget = resolveRemoteTarget(
    composerTarget,
    targetOptions,
    recent,
    pending,
    targetApprovalId,
  );
  const activeSession = selectedSession || sessionOptions[0] || "";
  const targetIsMissing =
    Boolean(targetApprovalId) &&
    loadedInitial &&
    !pending.some((approval) => approval.approval_id === targetApprovalId);

  if (replacePairing) {
    return (
      <main className="app-shell">
        <PairingView
          onPaired={(next) => {
            window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
            window.location.reload();
          }}
        />
        <button type="button" className="ghost-button" onClick={() => setReplacePairing(false)}>
          Cancel
        </button>
      </main>
    );
  }

  return (
    <main className="app-shell">
      <header className="top-bar">
        <div>
          <p className="eyebrow">Onibi · {connection.scope === "read-only" ? "Read only" : "Full access"}</p>
          <h1>{shortMachineId(connection.machineId)}</h1>
          <dl className="identity-grid" aria-label="Paired machine identity">
            <div>
              <dt>Machine</dt>
              <dd>{connection.machineId}</dd>
            </div>
            <div>
              <dt>Device</dt>
              <dd>{connection.deviceId}</dd>
            </div>
            <div>
              <dt>Transport</dt>
              <dd>{activeTransport}</dd>
            </div>
          </dl>
        </div>
        <div className="top-actions">
          <span className={`ws-pill ${wsState}`}>{wsState}</span>
          {connection.scope === "full" ? (
            <button
              type="button"
              className="danger-button"
              disabled={wsState !== "open" || killBusy}
              onClick={() => setKillConfirming(true)}
            >
              Stop all
            </button>
          ) : (
            <span className="scope-pill">Read only</span>
          )}
          <button type="button" className="ghost-button" onClick={() => setReplacePairing(true)}>
            Pair another
          </button>
          <button type="button" className="ghost-button" onClick={onForget}>
            Unpair
          </button>
        </div>
      </header>
      {connection.scope === "full" && killConfirming ? (
        <section className="kill-switch-panel" aria-label="Confirm stop all agents">
          <div>
            <strong>Stop all agents on this machine?</strong>
            <p>Running terminals will be killed and pending approvals will be denied.</p>
          </div>
          <div className="approval-actions">
            <button
              type="button"
              className="ghost-button"
              disabled={killBusy}
              onClick={() => setKillConfirming(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="danger-button"
              disabled={killBusy}
              onClick={() => void emergencyStop()}
            >
              {killBusy ? "Stopping..." : "Stop all agents"}
            </button>
          </div>
        </section>
      ) : null}
      {killStatus ? <p className="status-line">{killStatus}</p> : null}

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
        <button className={tab === "send" ? "active" : ""} onClick={() => setTab("send")}>
          Send <span>{targetOptions.length}</span>
        </button>
      </nav>

      {error ? <p className="error-line">{error}</p> : null}
      {wsState !== "open" ? (
        <p className="status-line">
          {connectionStateMessage(wsState, activeTransport)}
          <button type="button" className="inline-retry" onClick={() => void loadInitial()}>
            Retry
          </button>
        </p>
      ) : null}
      {targetIsMissing ? (
        <p className="status-line target-missing">
          Target approval is no longer pending.
          <button
            type="button"
            className="inline-retry"
            onClick={() => {
              window.history.replaceState({}, "", "/m/");
              setTargetApprovalId(null);
            }}
          >
            Clear
          </button>
        </p>
      ) : null}

      {tab === "pending" ? (
        <section className="approval-list">
          {pending.length === 0 ? (
            <EmptyState title="No pending approvals" body="New agent tool calls appear here." />
          ) : (
            pending.map((approval) => (
              <ApprovalCard
                key={approval.approval_id}
                approval={approval}
                targeted={approval.approval_id === targetApprovalId}
                readOnly={connection.scope === "read-only"}
                onDecide={decide}
                onReply={() => {
                  setComposerTarget(approval.session_id);
                  setTab("send");
                }}
              />
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

      {tab === "send" ? (
        <RemoteKeystrokeComposer
          readOnly={connection.scope === "read-only"}
          targets={targetOptions}
          targetId={resolvedComposerTarget}
          onTargetChange={setComposerTarget}
          fetchWithFallback={fetchWithFallback}
          onSent={() => void loadInitial()}
        />
      ) : null}
    </main>
  );
}

function ApprovalCard({
  approval,
  targeted,
  readOnly,
  onDecide,
  onReply,
}: {
  approval: Approval;
  targeted?: boolean;
  readOnly?: boolean;
  onDecide: (
    approval: Approval,
    decision: Decision,
    editedCommand?: string,
    reason?: string,
  ) => Promise<void>;
  onReply: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [command, setCommand] = useState(commandText(approval.input));
  const [denyReason, setDenyReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [swipeX, setSwipeX] = useState(0);
  const swipeStart = useRef<number | null>(null);
  const risks = approvalRiskBadges(approval, command);
  const swipeDisabled = editing || busy || readOnly;

  const submit = async (decision: Decision, editedCommand?: string, reason?: string) => {
    if (readOnly) {
      return;
    }
    setBusy(true);
    try {
      await onDecide(approval, decision, editedCommand, reason);
    } finally {
      setBusy(false);
    }
  };

  const onPointerDown = (event: PointerEvent<HTMLElement>) => {
    if (swipeDisabled) {
      return;
    }
    swipeStart.current = event.clientX;
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    if (swipeStart.current === null || swipeDisabled) {
      return;
    }
    setSwipeX(Math.max(-140, Math.min(140, event.clientX - swipeStart.current)));
  };

  const onPointerEnd = (event: PointerEvent<HTMLElement>) => {
    if (swipeStart.current === null) {
      return;
    }
    const delta = event.clientX - swipeStart.current;
    swipeStart.current = null;
    setSwipeX(0);
    const decision = swipeDecision(delta, event.currentTarget.clientWidth);
    if (decision === "allow") {
      void submit("allow");
    } else if (decision === "deny") {
      void submit("deny", undefined, denyReason.trim());
    }
  };

  return (
    <article
      className={`approval-card${targeted ? " targeted" : ""}${readOnly ? " read-only" : ""}`}
      style={{ transform: swipeX ? `translateX(${swipeX}px)` : undefined }}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={() => {
        swipeStart.current = null;
        setSwipeX(0);
      }}
    >
      <div className="approval-header">
        <div>
          <p className="eyebrow">{approval.agent}</p>
          <h2>{approval.tool}</h2>
        </div>
        <time>{formatTime(approval.created_at)}</time>
      </div>
      <p className="cwd">{approval.cwd}</p>
      {risks.length > 0 ? (
        <div className="risk-list" aria-label="Approval risk indicators">
          {risks.map((risk) => <span key={risk}>{risk}</span>)}
        </div>
      ) : null}
      {editing && !readOnly ? (
        <div className="edit-panel">
          <textarea
            value={command}
            onChange={(event) => setCommand(event.target.value)}
            rows={5}
            aria-label="Edited command"
            autoFocus
          />
          <button type="button" className="ghost-button" onClick={() => {
            setCommand(commandText(approval.input));
            setEditing(false);
          }}>
            Cancel edit
          </button>
        </div>
      ) : (
        <pre>{command}</pre>
      )}
      {!editing && !readOnly ? (
        <textarea
          value={denyReason}
          onChange={(event) => setDenyReason(event.target.value)}
          rows={2}
          aria-label="Deny reason"
          placeholder="Deny reason (optional)"
        />
      ) : null}
      {readOnly ? <p className="readonly-note">Read-only spectator session. Decisions are disabled.</p> : null}
      {!readOnly ? <div className="approval-actions">
        <button type="button" className="ghost-button" disabled={busy} onClick={onReply}>
          Reply
        </button>
        <button type="button" disabled={busy} onClick={() => void submit("allow")}>
          Allow
        </button>
        {editing ? (
          <button type="button" disabled={busy} onClick={() => void submit("allow", command)}>
            Allow edited
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
          onClick={() => void submit("deny", undefined, denyReason.trim())}
        >
          Deny
        </button>
      </div> : null}
    </article>
  );
}

function RemoteKeystrokeComposer({
  readOnly,
  targets,
  targetId,
  onTargetChange,
  fetchWithFallback,
  onSent,
}: {
  readOnly: boolean;
  targets: PaneTarget[];
  targetId: string;
  onTargetChange: (targetId: string) => void;
  fetchWithFallback: (path: string, init?: RequestInit) => Promise<Response>;
  onSent: () => void;
}) {
  const [text, setText] = useState("");
  const [sendEnter, setSendEnter] = useState(true);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [pendingDispatch, setPendingDispatch] = useState<PendingRemoteDispatch | null>(null);
  const selectedTarget = targets.find((target) => target.paneId === targetId);
  const disabled = readOnly || busy || !selectedTarget;

  const dispatch = async (next: PendingRemoteDispatch, confirmed: boolean) => {
    if (readOnly) {
      setStatus("Read-only spectator session. Remote typing is disabled.");
      return;
    }
    if (!selectedTarget) {
      setStatus("Choose a target pane.");
      return;
    }
    if (!confirmed && needsRemoteConfirmation(selectedTarget, next.destructive)) {
      setPendingDispatch(next);
      setStatus(null);
      return;
    }
    setBusy(true);
    setStatus(null);
    try {
      const path =
        next.kind === "text"
          ? `/v1/panes/${encodeURIComponent(selectedTarget.paneId)}/send-text`
          : `/v1/panes/${encodeURIComponent(selectedTarget.paneId)}/send-keys`;
      const body =
        next.kind === "text"
          ? sendRemoteTextRequest(text, sendEnter, confirmed)
          : sendRemotePresetRequest(next.preset?.key ?? "", confirmed);
      const response = await fetchWithFallback(path, {
        method: "POST",
        body: JSON.stringify(body),
      });
      const result = (await response.json()) as PaneSendResponse;
      setPendingDispatch(null);
      setStatus(`Sent ${result.bytes} bytes to ${selectedTarget.label}.`);
      if (next.kind === "text") {
        setText("");
      }
      onSent();
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : String(caught);
      if (message.includes("HTTP 409")) {
        setPendingDispatch(next);
        setStatus(null);
      } else {
        setStatus(message);
      }
    } finally {
      setBusy(false);
    }
  };

  const submitText = () => {
    const hasPayload = text.length > 0 || sendEnter;
    if (!hasPayload) {
      setStatus("Text or Enter is required.");
      return;
    }
    void dispatch(
      {
        kind: "text",
        destructive: false,
        message: "Send text to this pane?",
      },
      false,
    );
  };

  return (
    <section className="remote-composer" aria-label="Send text to pane">
      <div className="composer-header">
        <div>
          <p className="eyebrow">Remote input</p>
          <h2>Send to pane</h2>
        </div>
        {readOnly ? <span className="scope-pill">Read only</span> : null}
      </div>
      <label>
        <span>Target</span>
        <select
          value={targetId}
          disabled={readOnly || busy || targets.length === 0}
          onChange={(event) => onTargetChange(event.target.value)}
          aria-label="Target pane"
        >
          {targets.length === 0 ? <option value="">No panes</option> : null}
          {targets.length > 0 ? <option value="">Choose target</option> : null}
          {targets.map((target) => (
            <option key={target.paneId} value={target.paneId}>
              {target.label} · {target.status} · {target.trustMode}
            </option>
          ))}
        </select>
      </label>
      <textarea
        value={text}
        disabled={disabled}
        onChange={(event) => setText(event.target.value)}
        rows={5}
        aria-label="Text to send"
        placeholder="Text to send"
      />
      <label className="toggle-row">
        <input
          type="checkbox"
          checked={sendEnter}
          disabled={disabled}
          onChange={(event) => setSendEnter(event.target.checked)}
        />
        <span>Send Enter</span>
      </label>
      <div className="preset-row" aria-label="Remote input presets">
        {REMOTE_PRESETS.map((preset) => (
          <button
            key={preset.key}
            type="button"
            className={preset.destructive ? "danger-button" : "ghost-button"}
            disabled={disabled}
            onClick={() =>
              void dispatch(
                {
                  kind: "preset",
                  preset,
                  destructive: preset.destructive,
                  message: `${preset.label} on this pane?`,
                },
                false,
              )
            }
          >
            {preset.label}
          </button>
        ))}
      </div>
      <div className="approval-actions">
        <button type="button" disabled={disabled} onClick={submitText}>
          Send text
        </button>
      </div>
      {pendingDispatch ? (
        <div className="confirm-sheet" role="alert">
          <strong>{confirmationMessageForDispatch(selectedTarget, pendingDispatch)}</strong>
          <div className="approval-actions">
            <button
              type="button"
              className="ghost-button"
              disabled={busy}
              onClick={() => setPendingDispatch(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              className={pendingDispatch.destructive ? "danger-button" : ""}
              disabled={busy}
              onClick={() => void dispatch(pendingDispatch, true)}
            >
              Confirm
            </button>
          </div>
        </div>
      ) : null}
      {readOnly ? (
        <p className="readonly-note">Read-only spectator session. Remote typing is disabled.</p>
      ) : null}
      {status ? <p className={status.includes("failed") ? "error-line" : "status-line"}>{status}</p> : null}
    </section>
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
  const standalone =
    typeof window !== "undefined" &&
    (window.matchMedia?.("(display-mode: standalone)").matches ||
      (navigator as Navigator & { standalone?: boolean }).standalone === true);
  const [installPrompt, setInstallPrompt] = useState<BeforeInstallPromptEvent | null>(null);
  const [installed, setInstalled] = useState(standalone);

  useEffect(() => {
    const onBeforeInstallPrompt = (event: Event) => {
      event.preventDefault();
      setInstallPrompt(event as BeforeInstallPromptEvent);
    };
    const onInstalled = () => {
      setInstalled(true);
      setInstallPrompt(null);
    };
    window.addEventListener("beforeinstallprompt", onBeforeInstallPrompt);
    window.addEventListener("appinstalled", onInstalled);
    return () => {
      window.removeEventListener("beforeinstallprompt", onBeforeInstallPrompt);
      window.removeEventListener("appinstalled", onInstalled);
    };
  }, []);

  const promptInstall = async () => {
    const prompt = installPrompt;
    if (!prompt) {
      return;
    }
    await prompt.prompt();
    await prompt.userChoice;
    setInstallPrompt(null);
  };

  return (
    <div className="onboarding">
      <strong>{installStateTitle({ standalone: installed, ios, installable: Boolean(installPrompt) })}</strong>
      <span>
        {installStateBody({ standalone: installed, ios, installable: Boolean(installPrompt) })}
      </span>
      {installPrompt && !installed ? (
        <button type="button" className="ghost-button" onClick={() => void promptInstall()}>
          Install
        </button>
      ) : null}
    </div>
  );
}

function pendingCacheKey(connection: Connection): string {
  return `${PENDING_CACHE_PREFIX}${connection.machineId}`;
}

function loadCachedPending(connection: Connection): Approval[] {
  if (!storageAvailable()) {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(pendingCacheKey(connection));
    return raw ? (JSON.parse(raw) as Approval[]) : [];
  } catch {
    return [];
  }
}

function cachePending(connection: Connection, pending: Approval[]) {
  if (!storageAvailable()) {
    return;
  }
  window.localStorage.setItem(pendingCacheKey(connection), JSON.stringify(pending));
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
      created_at: typeof message.created_at === "number" ? message.created_at : Date.now(),
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
        scope: (url.searchParams.get("scope") as ClientScope | null) ?? undefined,
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
  const transport = payload.transports?.find((item) => phoneReachableUrl(item.url))
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

export function connectionStateMessage(state: WsState, transport: string): string {
  switch (state) {
    case "connecting":
      return `Connecting through ${transport}...`;
    case "fallback":
      return `Trying fallback transport after ${transport} failed...`;
    case "closed":
      return `Offline on ${transport}. Cached approvals stay visible.`;
    case "idle":
      return `Waiting for realtime connection on ${transport}.`;
    default:
      return "";
  }
}

export function installStateTitle({
  standalone,
  ios,
  installable,
}: {
  standalone: boolean;
  ios: boolean;
  installable: boolean;
}): string {
  if (standalone) {
    return "Installed PWA.";
  }
  if (ios) {
    return "Add to Home Screen for push.";
  }
  return installable ? "Ready to install." : "Installable PWA.";
}

export function installStateBody({
  standalone,
  ios,
  installable,
}: {
  standalone: boolean;
  ios: boolean;
  installable: boolean;
}): string {
  if (standalone) {
    return "Lock-screen notification deep links open this paired machine.";
  }
  if (ios) {
    return "Open from Safari, share, then Add to Home Screen.";
  }
  return installable
    ? "Install this app for persistent notifications."
    : "Install from the browser menu for persistent notifications.";
}

export function swipeDecision(deltaX: number, width: number): Decision | null {
  const threshold = Math.max(96, width * 0.28);
  if (deltaX >= threshold) {
    return "allow";
  }
  if (deltaX <= -threshold) {
    return "deny";
  }
  return null;
}

export function buildDecisionBody(
  approval: Approval,
  decision: Decision,
  editedCommand?: string,
  reason?: string,
) {
  const body: {
    decision: Decision;
    by: string;
    reason?: string;
    updatedInput?: unknown;
  } = { decision, by: "mobile" };
  if (decision === "deny") {
    body.reason = reason?.trim() || "denied from mobile";
  }
  if (decision === "allow" && editedCommand !== undefined) {
    body.updatedInput = editedInput(approval.input, editedCommand);
    body.reason = "edited from mobile";
  }
  return body;
}

export function emergencyStopRequest(): RequestInit {
  return {
    method: "POST",
    body: "{}",
  };
}

export function candidateBaseUrls(connection: Connection): string[] {
  return uniqueStrings([
    connection.baseUrl,
    ...connection.transports
      .filter((transport) => phoneReachableUrl(transport.url))
      .map((transport) => normalizeBaseUrl(transport.url)),
    ...connection.transports.map((transport) => normalizeBaseUrl(transport.url)),
  ]);
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

function approvalRiskBadges(approval: Approval, text: string): string[] {
  const lower = text.toLowerCase();
  const badges = new Set<string>();
  if (/\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)\b/.test(lower) || /\brm\s+-rf\b/.test(lower)) {
    badges.add("Destructive delete");
  }
  if (/\bsudo\b/.test(lower)) {
    badges.add("Elevated command");
  }
  if (/\b(curl|wget)\b.*\|\s*(sh|bash|zsh)\b/.test(lower)) {
    badges.add("Network script");
  }
  if (approval.cwd && /(\s|^)(\/|~\/|\.\.\/)/.test(text) && !text.includes(approval.cwd)) {
    badges.add("Outside cwd");
  }
  return [...badges];
}

export function buildPaneTargetOptions(
  paneTargets: PaneTarget[],
  recent: RunEvent[],
  terminalOutput: Record<string, string[]>,
  pending: Approval[],
): PaneTarget[] {
  const targets = new Map<string, PaneTarget>();
  for (const target of paneTargets) {
    targets.set(target.paneId, target);
  }
  for (const approval of pending) {
    if (!approval.session_id || targets.has(approval.session_id)) {
      continue;
    }
    targets.set(approval.session_id, {
      paneId: approval.session_id,
      sessionId: approval.session_id,
      label: `${approval.agent} ${approval.tool}`,
      agent: approval.agent,
      cwd: approval.cwd,
      status: "blocked",
      trustMode: "approval-required",
    });
  }
  for (const event of recent) {
    if (!event.session_id || targets.has(event.session_id)) {
      continue;
    }
    targets.set(event.session_id, {
      paneId: event.session_id,
      sessionId: event.session_id,
      label: event.session_id,
      status: event.kind,
      trustMode: "approval-required",
    });
  }
  for (const sessionId of Object.keys(terminalOutput).sort()) {
    if (targets.has(sessionId)) {
      continue;
    }
    targets.set(sessionId, {
      paneId: sessionId,
      sessionId,
      label: sessionId,
      status: "mirrored",
      trustMode: "approval-required",
    });
  }
  return [...targets.values()];
}

export function resolveRemoteTarget(
  explicitTarget: string,
  targets: PaneTarget[],
  recent: RunEvent[],
  pending: Approval[],
  targetApprovalId: string | null,
): string {
  if (explicitTarget && targets.some((target) => target.paneId === explicitTarget)) {
    return explicitTarget;
  }
  const approvalTarget = pending.find(
    (approval) => approval.approval_id === targetApprovalId && approval.session_id,
  )?.session_id;
  if (approvalTarget && targets.some((target) => target.paneId === approvalTarget)) {
    return approvalTarget;
  }
  const recentTarget = recent.find((event) =>
    targets.some((target) => target.paneId === event.session_id),
  )?.session_id;
  if (recentTarget) {
    return recentTarget;
  }
  return targets.length === 1 ? targets[0].paneId : "";
}

export function needsRemoteConfirmation(target: PaneTarget | undefined, destructive: boolean): boolean {
  return Boolean(target && (destructive || target.trustMode === "approval-required"));
}

export function sendRemoteTextRequest(text: string, sendEnter: boolean, confirmed: boolean) {
  return {
    text,
    sendEnter,
    confirmed,
  };
}

export function sendRemotePresetRequest(preset: string, confirmed: boolean) {
  return {
    preset,
    confirmed,
  };
}

function confirmationMessageForDispatch(
  target: PaneTarget | undefined,
  dispatch: PendingRemoteDispatch,
): string {
  if (dispatch.destructive) {
    return `${dispatch.message} This preset can interrupt the running process.`;
  }
  if (target?.trustMode === "approval-required") {
    return `${dispatch.message} This session requires approval confirmation.`;
  }
  return dispatch.message;
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

function phoneReachableUrl(url: string): boolean {
  return !/\/\/(127\.0\.0\.1|localhost|\[::1\])(?::|\/|$)/.test(url);
}

function uniqueStrings(items: string[]): string[] {
  return [...new Set(items.filter(Boolean).map(normalizeBaseUrl))];
}

function nextBaseUrl(current: string, candidates: string[]): string {
  const normalized = normalizeBaseUrl(current);
  const list = uniqueStrings(candidates);
  if (list.length <= 1) {
    return normalized;
  }
  const index = list.indexOf(normalized);
  return list[(index + 1 + list.length) % list.length] ?? normalized;
}

function transportLabelForUrl(connection: Connection, baseUrl: string): string {
  const normalized = normalizeBaseUrl(baseUrl);
  const transport = connection.transports.find(
    (item) => normalizeBaseUrl(item.url) === normalized,
  );
  return transport ? `${transport.name} ${normalized}` : normalized;
}

function shortMachineId(machineId: string): string {
  return machineId.length > 18 ? `${machineId.slice(0, 8)}...${machineId.slice(-6)}` : machineId;
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
