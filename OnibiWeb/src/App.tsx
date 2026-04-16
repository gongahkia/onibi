import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  APIError,
  fetchBootstrap,
  fetchSessionBuffer,
  fetchSessions,
  normalizeConnectionConfig,
  sendSessionInput,
  toUserFacingConnectionError
} from "./api/httpClient";
import { RealtimeClient, type RealtimeConnectionState } from "./api/realtimeClient";
import {
  appendChunk,
  mergeBufferSnapshot,
  removeSession,
  replaceBuffer,
  sortSessionsByRecentActivity,
  type OutputBySession,
  type OutputEntry,
  upsertSession
} from "./store/sessionStore";
import type {
  ConnectionConfig,
  ControllableSessionSnapshot,
  RealtimeServerMessage,
  RemoteInputKey
} from "./types";
import { ConnectionView } from "./views/ConnectionView";
import { LiveSessionView } from "./views/LiveSessionView";
import { SessionsView } from "./views/SessionsView";

const STORAGE_KEY = "onibi-web.connection";

type Route =
  | { kind: "connect" }
  | { kind: "sessions" }
  | { kind: "live"; sessionId: string };

function parseRoute(pathname: string): Route {
  if (pathname === "/connect") {
    return { kind: "connect" };
  }
  if (pathname === "/sessions") {
    return { kind: "sessions" };
  }
  const liveMatch = pathname.match(/^\/sessions\/([^/]+)$/);
  if (liveMatch) {
    return { kind: "live", sessionId: decodeURIComponent(liveMatch[1]) };
  }
  return { kind: "sessions" };
}

function routePath(route: Route): string {
  if (route.kind === "connect") {
    return "/connect";
  }
  if (route.kind === "sessions") {
    return "/sessions";
  }
  return `/sessions/${encodeURIComponent(route.sessionId)}`;
}

function loadStoredConnection(): ConnectionConfig | null {
  const raw = window.localStorage.getItem(STORAGE_KEY);
  if (!raw) {
    return null;
  }
  try {
    const parsed = JSON.parse(raw) as ConnectionConfig;
    if (!parsed.baseURL || !parsed.token) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

function persistConnection(connection: ConnectionConfig): void {
  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(connection));
}

function clearPersistedConnection(): void {
  window.localStorage.removeItem(STORAGE_KEY);
}

function nextClientRequestID(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return String(Date.now());
}

export default function App(): JSX.Element {
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.pathname));
  const [connection, setConnection] = useState<ConnectionConfig | null>(() => loadStoredConnection());
  const [sessions, setSessions] = useState<ControllableSessionSnapshot[]>([]);
  const [outputBySession, setOutputBySession] = useState<OutputBySession>({});
  const [connectionError, setConnectionError] = useState<string | null>(null);
  const [inputError, setInputError] = useState<string | null>(null);
  const [realtimeError, setRealtimeError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);
  const [realtimeState, setRealtimeState] = useState<RealtimeConnectionState>("disconnected");

  const realtimeRef = useRef<RealtimeClient | null>(null);
  const subscribedSessionIDRef = useRef<string | null>(null);

  const navigate = useCallback((nextRoute: Route) => {
    const path = routePath(nextRoute);
    if (window.location.pathname !== path) {
      window.history.pushState({}, "", path);
    }
    setRoute(nextRoute);
  }, []);

  useEffect(() => {
    const onPopState = () => {
      setRoute(parseRoute(window.location.pathname));
    };
    window.addEventListener("popstate", onPopState);
    return () => {
      window.removeEventListener("popstate", onPopState);
    };
  }, []);

  const disconnectRealtime = useCallback(() => {
    realtimeRef.current?.disconnect();
    realtimeRef.current = null;
    setRealtimeState("disconnected");
  }, []);

  const handleRealtimeMessage = useCallback((message: RealtimeServerMessage) => {
    switch (message.type) {
      case "sessions_snapshot":
        if (message.sessions) {
          setSessions(sortSessionsByRecentActivity(message.sessions));
        }
        return;
      case "session_added":
      case "session_updated":
        if (message.session) {
          setSessions((existing) => upsertSession(existing, message.session!));
        }
        return;
      case "session_removed":
        if (message.sessionId) {
          setSessions((existing) => removeSession(existing, message.sessionId!));
          setOutputBySession((existing) => {
            const copy = { ...existing };
            delete copy[message.sessionId!];
            return copy;
          });
        }
        return;
      case "buffer_snapshot":
        if (message.sessionId && message.chunks) {
          setOutputBySession((existing) =>
            mergeBufferSnapshot(existing, message.sessionId!, message.chunks!, message.bufferCursor ?? null)
          );
        }
        return;
      case "output":
        if (message.chunk) {
          setOutputBySession((existing) => appendChunk(existing, message.chunk!));
        }
        return;
      case "error":
        setRealtimeError(`${message.code ?? "error"}: ${message.message ?? "unknown"}`);
        return;
      default:
        return;
    }
  }, []);

  useEffect(() => {
    if (!connection) {
      disconnectRealtime();
      subscribedSessionIDRef.current = null;
      setSessions([]);
      setOutputBySession({});
      if (route.kind !== "connect") {
        navigate({ kind: "connect" });
      }
      return;
    }

    let isCancelled = false;
    setConnecting(true);
    setConnectionError(null);
    setRealtimeError(null);

    const connect = async () => {
      try {
        const bootstrap = await fetchBootstrap(connection);
        if (isCancelled) {
          return;
        }
        setSessions(sortSessionsByRecentActivity(bootstrap.sessions));
        persistConnection(connection);
        if (window.location.pathname === "/connect") {
          navigate({ kind: "sessions" });
        }

        const realtime = new RealtimeClient({
          connection,
          onStateChange: (state) => {
            if (!isCancelled) {
              setRealtimeState(state);
            }
          },
          onMessage: (message) => {
            if (!isCancelled) {
              handleRealtimeMessage(message);
            }
          },
          onError: (errorMessage) => {
            if (!isCancelled) {
              setRealtimeError(errorMessage);
            }
          }
        });
        realtimeRef.current = realtime;
        realtime.connect();
      } catch (error) {
        if (isCancelled) {
          return;
        }
        disconnectRealtime();
        setConnectionError(toUserFacingConnectionError(error));
        if (error instanceof APIError && error.statusCode === 401) {
          setSessions([]);
        }
        navigate({ kind: "connect" });
      } finally {
        if (!isCancelled) {
          setConnecting(false);
        }
      }
    };

    connect();

    return () => {
      isCancelled = true;
      disconnectRealtime();
    };
  }, [connection, disconnectRealtime, handleRealtimeMessage, navigate]);

  useEffect(() => {
    if (!connection) {
      return;
    }

    const intervalID = window.setInterval(async () => {
      try {
        const snapshot = await fetchSessions(connection);
        setSessions(sortSessionsByRecentActivity(snapshot));
      } catch {
        // Realtime path remains primary; polling is fallback.
      }
    }, 6000);

    return () => {
      window.clearInterval(intervalID);
    };
  }, [connection]);

  const activeSession = useMemo(() => {
    if (route.kind !== "live") {
      return null;
    }
    return sessions.find((session) => session.id === route.sessionId) ?? null;
  }, [route, sessions]);

  const activeOutputEntries = useMemo<OutputEntry[]>(() => {
    if (!activeSession) {
      return [];
    }
    return outputBySession[activeSession.id] ?? [];
  }, [activeSession, outputBySession]);

  useEffect(() => {
    if (realtimeState !== "authenticated") {
      subscribedSessionIDRef.current = null;
      return;
    }

    const realtime = realtimeRef.current;
    if (!realtime) {
      return;
    }

    const previousSessionID = subscribedSessionIDRef.current;
    const activeSessionID = route.kind === "live" ? route.sessionId : null;
    if (previousSessionID && previousSessionID !== activeSessionID) {
      realtime.send({
        type: "unsubscribe",
        sessionId: previousSessionID
      });
    }

    if (activeSessionID) {
      realtime.send({
        type: "subscribe",
        sessionId: activeSessionID
      });
      realtime.send({
        type: "request_buffer",
        sessionId: activeSessionID
      });
    }

    subscribedSessionIDRef.current = activeSessionID;
  }, [route, realtimeState]);

  const connectToHost = (candidate: ConnectionConfig) => {
    setConnection(normalizeConnectionConfig(candidate));
    setConnectionError(null);
    setInputError(null);
    setRealtimeError(null);
  };

  const clearConnection = () => {
    clearPersistedConnection();
    subscribedSessionIDRef.current = null;
    setConnection(null);
    setConnectionError(null);
    setInputError(null);
    setRealtimeError(null);
    navigate({ kind: "connect" });
  };

  const refreshSessions = async () => {
    if (!connection) {
      return;
    }
    try {
      const snapshot = await fetchSessions(connection);
      setSessions(sortSessionsByRecentActivity(snapshot));
    } catch (error) {
      setConnectionError(toUserFacingConnectionError(error));
    }
  };

  const reloadSessionBuffer = async (sessionId: string) => {
    if (!connection) {
      return;
    }
    try {
      const snapshot = await fetchSessionBuffer(connection, sessionId);
      setOutputBySession((existing) => replaceBuffer(existing, sessionId, snapshot.chunks));
    } catch (error) {
      setInputError(toUserFacingConnectionError(error));
    }
  };

  const sendKey = async (sessionId: string, key: RemoteInputKey) => {
    setInputError(null);
    const realtime = realtimeRef.current;
    if (realtime && realtimeState === "authenticated") {
      realtime.send({
        type: "send_input",
        sessionId,
        kind: "key",
        key,
        clientRequestId: nextClientRequestID()
      });
      return;
    }

    if (!connection) {
      return;
    }
    try {
      await sendSessionInput(connection, sessionId, {
        kind: "key",
        key
      });
    } catch (error) {
      setInputError(toUserFacingConnectionError(error));
    }
  };

  const sendLine = async (sessionId: string, text: string) => {
    setInputError(null);
    const realtime = realtimeRef.current;
    if (realtime && realtimeState === "authenticated") {
      realtime.send({
        type: "send_input",
        sessionId,
        kind: "text",
        text,
        clientRequestId: nextClientRequestID()
      });
      realtime.send({
        type: "send_input",
        sessionId,
        kind: "key",
        key: "enter",
        clientRequestId: nextClientRequestID()
      });
      return;
    }

    if (!connection) {
      return;
    }

    try {
      await sendSessionInput(connection, sessionId, {
        kind: "text",
        text
      });
      await sendSessionInput(connection, sessionId, {
        kind: "key",
        key: "enter"
      });
    } catch (error) {
      setInputError(toUserFacingConnectionError(error));
    }
  };

  const sendTerminalData = (sessionId: string, data: string) => {
    setInputError(null);
    const realtime = realtimeRef.current;
    if (!realtime || realtimeState !== "authenticated") {
      setInputError("Realtime disconnected. Use reconnect or send commands via the input bar.");
      return;
    }

    realtime.send({
      type: "send_input",
      sessionId,
      kind: "text",
      text: data,
      clientRequestId: nextClientRequestID()
    });
  };

  const sendTerminalResize = (sessionId: string, cols: number, rows: number) => {
    const realtime = realtimeRef.current;
    if (!realtime || realtimeState !== "authenticated") {
      return;
    }

    realtime.send({
      type: "resize",
      sessionId,
      cols,
      rows
    });
  };

  const primaryError = connectionError ?? realtimeError;
  const storedConnection = connection ?? loadStoredConnection();

  if (route.kind === "connect" || !connection) {
    return (
      <ConnectionView
        initialConnection={storedConnection}
        connecting={connecting}
        errorMessage={primaryError}
        onConnect={connectToHost}
        onClearSaved={clearConnection}
      />
    );
  }

  if (route.kind === "sessions") {
    return (
      <SessionsView
        sessions={sessions}
        realtimeState={realtimeState}
        onOpenSession={(sessionId) => {
          setInputError(null);
          navigate({ kind: "live", sessionId });
        }}
        onRefresh={refreshSessions}
      />
    );
  }

  return (
    <LiveSessionView
      session={activeSession}
      outputEntries={activeOutputEntries}
      realtimeState={realtimeState}
      inputError={inputError}
      onBack={() => navigate({ kind: "sessions" })}
      onReloadBuffer={() => reloadSessionBuffer(route.sessionId)}
      onSendLine={(text) => sendLine(route.sessionId, text)}
      onSendKey={(key) => sendKey(route.sessionId, key)}
      onTerminalInput={(data) => sendTerminalData(route.sessionId, data)}
      onTerminalResize={(cols, rows) => sendTerminalResize(route.sessionId, cols, rows)}
    />
  );
}
