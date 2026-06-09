import { useCallback, useEffect, useMemo, useState } from "react";
import { buildDecisionBody } from "../approvals";
import { cachePending, loadCachedPending } from "../connections";
import { authedFetch, shouldMarkAuthInvalid } from "../http";
import {
  candidateBaseUrls,
  nextBaseUrl,
  transportLabelForUrl,
  uniqueStrings,
} from "../pairing";
import {
  applyServerMessage,
  connectionStateMessage,
  realtimeUrl,
  reconnectDelay,
} from "../realtime";
import {
  buildPaneTargetOptions,
  emergencyStopRequest,
  resolveRemoteTarget,
} from "../remote-pane";
import type {
  Approval,
  Connection,
  Decision,
  PaneTarget,
  PaneTargetsResponse,
  RunEvent,
  ServerMessage,
  Tab,
  WsState,
} from "../types";
import { formatTime, shortMachineId } from "../utils";
import { ApprovalCard } from "./ApprovalCard";
import { ConnectionSwitcher } from "./ConnectionSwitcher";
import { EmptyState } from "./EmptyState";
import { PairingView } from "./PairingView";
import { RemoteKeystrokeComposer } from "./RemoteKeystrokeComposer";
import { TerminalMirrorView } from "./TerminalMirrorView";

export function InboxView({
  connection,
  connections,
  onPaired,
  onForget,
  onSelectConnection,
  onAuthInvalid,
}: {
  connection: Connection;
  connections: Connection[];
  onPaired: (connection: Connection) => void;
  onForget: () => void;
  onSelectConnection: (id: string) => void;
  onAuthInvalid: (message: string) => void;
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
          if (shouldMarkAuthInvalid(caught, connection.scope)) {
            const message = caught instanceof Error ? caught.message : "Pairing token rejected.";
            onAuthInvalid(message);
            throw caught;
          }
          lastError = caught;
        }
      }
      throw lastError instanceof Error ? lastError : new Error("No paired transport is reachable.");
    },
    [activeBaseUrl, baseUrlCandidates, connection.scope, connection.token, onAuthInvalid],
  );

  useEffect(() => {
    setActiveBaseUrl(connection.baseUrl);
  }, [connection.baseUrl, connection.id]);

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
      socket.onclose = (event) => {
        setWsState("closed");
        if (event.code === 1008) {
          onAuthInvalid("Realtime token rejected. Pair this machine again.");
          return;
        }
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
  }, [activeBaseUrl, baseUrlCandidates, connection, onAuthInvalid]);

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
          embedded
          onPaired={(next) => {
            onPaired(next);
            setReplacePairing(false);
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
          <ConnectionSwitcher
            connections={connections}
            activeId={connection.id}
            onSelect={onSelectConnection}
            onForget={onForget}
          />
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
          Inbox <span>{pending.length}</span>
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
