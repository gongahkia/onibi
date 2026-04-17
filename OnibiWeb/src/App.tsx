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
const REMEMBER_TOKEN_KEY = "onibi-web.remember-token";

type Route =
  | { kind: "connect" }
  | { kind: "sessions" }
  | { kind: "live"; sessionId: string };

function parseRoute(pathname: string): Route {
  if (pathname === "/" || pathname === "/connect") {
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
    return "/";
  }
  if (route.kind === "sessions") {
    return "/sessions";
  }
  return `/sessions/${encodeURIComponent(route.sessionId)}`;
}

function loadStoredConnection(): ConnectionConfig | null {
  const candidates = [
    window.sessionStorage.getItem(STORAGE_KEY),
    window.localStorage.getItem(STORAGE_KEY)
  ];

  for (const raw of candidates) {
    if (!raw) {
      continue;
    }
    try {
      const parsed = JSON.parse(raw) as ConnectionConfig;
      if (!parsed.baseURL || !parsed.token) {
        continue;
      }
      return parsed;
    } catch {
      continue;
    }
  }

  return null;
}

function loadRememberTokenPreference(): boolean {
  try {
    return window.localStorage.getItem(REMEMBER_TOKEN_KEY) === "1";
  } catch {
    return false;
  }
}

function persistConnection(connection: ConnectionConfig, rememberToken: boolean): void {
  const encoded = JSON.stringify(connection);
  if (rememberToken) {
    window.localStorage.setItem(STORAGE_KEY, encoded);
    window.localStorage.setItem(REMEMBER_TOKEN_KEY, "1");
    window.sessionStorage.removeItem(STORAGE_KEY);
    return;
  }

  window.sessionStorage.setItem(STORAGE_KEY, encoded);
  window.localStorage.setItem(REMEMBER_TOKEN_KEY, "0");
  window.localStorage.removeItem(STORAGE_KEY);
}

function clearPersistedConnection(): void {
  window.localStorage.removeItem(STORAGE_KEY);
  window.localStorage.removeItem(REMEMBER_TOKEN_KEY);
  window.sessionStorage.removeItem(STORAGE_KEY);
}

function clearPersistedTokenOnly(): void {
  window.localStorage.removeItem(STORAGE_KEY);
  window.sessionStorage.removeItem(STORAGE_KEY);
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
  const [rememberToken, setRememberToken] = useState<boolean>(() => loadRememberTokenPreference());
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

  const handleUnauthorizedToken = useCallback(
    (message: string) => {
      clearPersistedTokenOnly();
      setConnection(null);
      setSessions([]);
      setOutputBySession({});
      setConnectionError(message);
      setRealtimeError(null);
      setInputError(null);
      subscribedSessionIDRef.current = null;
      navigate({ kind: "connect" });
    },
    [navigate]
  );

  const handleRealtimeMessage = useCallback(
    (message: RealtimeServerMessage) => {
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
          if (message.code === "unauthorized") {
            handleUnauthorizedToken("Pairing token expired. Paste the latest token from the Mac host.");
            return;
          }
          setRealtimeError(`${message.code ?? "error"}: ${message.message ?? "unknown"}`);
          return;
        default:
          return;
      }
    },
    [handleUnauthorizedToken]
  );

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
        persistConnection(connection, rememberToken);
        if (window.location.pathname === "/connect" || window.location.pathname === "/") {
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
        if (error instanceof APIError && error.statusCode === 401) {
          handleUnauthorizedToken("Pairing token is invalid. Paste the latest token from the Mac host.");
          return;
        }
        setConnectionError(toUserFacingConnectionError(error));
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
  }, [connection, disconnectRealtime, handleRealtimeMessage, handleUnauthorizedToken, navigate, rememberToken]);

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

  const connectToHost = (candidate: ConnectionConfig, rememberTokenValue: boolean) => {
    setRememberToken(rememberTokenValue);
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
    setRememberToken(false);
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
      if (error instanceof APIError && error.statusCode === 401) {
        handleUnauthorizedToken("Pairing token expired. Paste the latest token from the Mac host.");
        return;
      }
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
      if (error instanceof APIError && error.statusCode === 401) {
        handleUnauthorizedToken("Pairing token expired. Paste the latest token from the Mac host.");
        return;
      }
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
      if (error instanceof APIError && error.statusCode === 401) {
        handleUnauthorizedToken("Pairing token expired. Paste the latest token from the Mac host.");
        return;
      }
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
      if (error instanceof APIError && error.statusCode === 401) {
        handleUnauthorizedToken("Pairing token expired. Paste the latest token from the Mac host.");
        return;
      }
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
        initialRememberToken={rememberToken}
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
        onDisconnect={clearConnection}
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
