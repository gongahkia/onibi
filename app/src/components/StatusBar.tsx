import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getGitStatus, type GitStatus } from "../lib/git";
import { dispatchOnibiNotification } from "../lib/notifications";
import { useSessionStore } from "../lib/sessions";
import { fetchTransportStatus, type TransportSnapshot } from "../lib/transports";

type TransportLoadState = "initial" | "ok" | "error";

function activeTransportLabel(
  transports: TransportSnapshot[],
  loadState: TransportLoadState,
): { label: string; running: boolean; icon: string; title: string } {
  if (loadState === "initial") {
    return {
      label: "checking",
      running: false,
      icon: "codicon-loading",
      title: "Checking phone transport status",
    };
  }
  if (loadState === "error") {
    return {
      label: "offline",
      running: false,
      icon: "codicon-error",
      title: "Transport status could not be fetched",
    };
  }
  const running = transports.find((t) => t.enabled && t.status.state === "running");
  if (running) return {
    label: running.label || running.name,
    running: true,
    icon: "codicon-broadcast",
    title: `Phone transport active: ${running.label || running.name}`,
  };
  const starting = transports.find((t) => t.status.state === "starting");
  if (starting) return {
    label: `${starting.label || starting.name}…`,
    running: false,
    icon: "codicon-sync",
    title: `Phone transport starting: ${starting.label || starting.name}`,
  };
  return {
    label: "no transport",
    running: false,
    icon: "codicon-circle-slash",
    title: "No transport is enabled for phone pairing",
  };
}

function basename(path: string | undefined | null): string {
  if (!path) return "";
  return path.split("/").filter(Boolean).pop() ?? path;
}

export function StatusBar() {
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selection = useSessionStore((state) => state.selectedFile);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const terminalFontSize = useSessionStore((state) => state.settings.terminalFontSize);

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeSessionId) ?? null,
    [sessions, activeSessionId],
  );
  const activeWorkspace = useMemo(() => {
    if (selection && "workspaceId" in selection) {
      return workspaces.find((w) => w.id === selection.workspaceId) ?? null;
    }
    return workspaces.find((w) => w.id === activeSession?.workspaceId) ?? null;
  }, [selection, workspaces, activeSession]);

  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [transports, setTransports] = useState<TransportSnapshot[]>([]);
  const [transportLoadState, setTransportLoadState] = useState<TransportLoadState>("initial");

  const refresh = useCallback(async () => {
    if (!activeWorkspace) {
      setGitStatus(null);
      return;
    }
    try {
      setGitStatus(await getGitStatus(activeWorkspace.path));
    } catch {
      setGitStatus(null);
    }
  }, [activeWorkspace]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const prevRunningRef = useRef<Set<string> | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function pull() {
      try {
        const snap = await fetchTransportStatus();
        if (cancelled) return;
        setTransports(snap);
        setTransportLoadState("ok");
        // detect state changes and toast
        const now = new Set(
          snap.filter((t) => t.status.state === "running").map((t) => t.name),
        );
        const prev = prevRunningRef.current;
        if (prev) {
          for (const t of snap) {
            const wasRunning = prev.has(t.name);
            const isRunning = now.has(t.name);
            if (!wasRunning && isRunning) {
              void dispatchOnibiNotification({
                source: "session",
                kind: "info",
                title: `${t.label || t.name} is reachable`,
                body: "Your phone can now pair with this machine.",
              });
            } else if (wasRunning && !isRunning) {
              const message = t.status.state === "failed" ? t.status.message : "Transport stopped.";
              void dispatchOnibiNotification({
                source: "session",
                kind: "info",
                title: `${t.label || t.name} went offline`,
                body: message,
              });
            }
          }
        }
        prevRunningRef.current = now;
      } catch {
        if (!cancelled) {
          setTransports([]);
          setTransportLoadState("error");
        }
      }
    }
    void pull();
    const id = window.setInterval(pull, 20_000); // poll every 20s
    const onFocus = () => void pull();
    window.addEventListener("focus", onFocus);
    return () => {
      cancelled = true;
      window.clearInterval(id);
      window.removeEventListener("focus", onFocus);
    };
  }, []);

  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, s) => sum + s.pendingApprovals.length, 0),
    [sessions],
  );

  const errorSessions = useMemo(
    () => sessions.filter((s) => s.status === "error").length,
    [sessions],
  );

  const variant = pendingApprovals > 0
    ? "attention"
    : errorSessions > 0
      ? "error"
      : "default";

  const branchLabel = gitStatus?.isRepo && gitStatus.branch
    ? `${gitStatus.branch}${gitStatus.ahead ? ` ↑${gitStatus.ahead}` : ""}${gitStatus.behind ? ` ↓${gitStatus.behind}` : ""}`
    : null;
  const changeCount = gitStatus?.entries.length ?? 0;
  const exitCode = activeSession?.lastExitCode;
  const cwdLabel = basename(activeSession?.cwd);

  function bumpFontSize(delta: number) {
    const next = Math.max(8, Math.min(32, terminalFontSize + delta));
    if (next !== terminalFontSize) {
      updateSettings({ terminalFontSize: next });
    }
  }

  return (
    <footer className="status-bar" data-variant={variant} role="contentinfo">
      <div className="status-bar-section left">
        {branchLabel ? (
          <button
            type="button"
            className="status-bar-item"
            onClick={() => setView("source-control")}
            title="Source Control"
          >
            <i className="codicon codicon-source-control" aria-hidden="true" />
            <span>{branchLabel}</span>
            {changeCount > 0 ? <span className="status-bar-count">{changeCount}</span> : null}
          </button>
        ) : null}
        {pendingApprovals > 0 ? (
          <button
            type="button"
            className="status-bar-item attention"
            onClick={() => setView("approvals")}
            title="Pending approvals"
          >
            <i className="codicon codicon-bell-dot" aria-hidden="true" />
            <span>{pendingApprovals} pending</span>
          </button>
        ) : null}
        {errorSessions > 0 ? (
          <span className="status-bar-item error" title="Sessions in error state">
            <i className="codicon codicon-error" aria-hidden="true" />
            <span>{errorSessions}</span>
          </span>
        ) : null}
      </div>
      <div className="status-bar-section right">
        {(() => {
          const t = activeTransportLabel(transports, transportLoadState);
          return (
            <span
              className={`status-bar-item ${t.running ? "ok" : ""}`}
              title={t.title}
            >
              <i
                className={`codicon ${t.icon}`}
                aria-hidden="true"
              />
              <span>{t.label}</span>
            </span>
          );
        })()}
        {cwdLabel ? (
          <span className="status-bar-item" title={activeSession?.cwd ?? ""}>
            <i className="codicon codicon-folder" aria-hidden="true" />
            <span>{cwdLabel}</span>
          </span>
        ) : null}
        {typeof exitCode === "number" ? (
          <span
            className={`status-bar-item ${exitCode === 0 ? "ok" : "error"}`}
            title={exitCode === 0 ? "Last command exited cleanly" : `Last command exited with code ${exitCode}`}
          >
            <i className={`codicon ${exitCode === 0 ? "codicon-pass" : "codicon-error"}`} aria-hidden="true" />
            <span>{exitCode === 0 ? "ok" : `exit ${exitCode}`}</span>
          </span>
        ) : null}
        <span className="status-bar-item" title="Encoding">UTF-8</span>
        <button
          type="button"
          className="status-bar-item"
          title="Decrease terminal font size"
          onClick={() => bumpFontSize(-1)}
        >
          <i className="codicon codicon-remove" aria-hidden="true" />
        </button>
        <span className="status-bar-item" title="Terminal font size">{terminalFontSize}px</span>
        <button
          type="button"
          className="status-bar-item"
          title="Increase terminal font size"
          onClick={() => bumpFontSize(1)}
        >
          <i className="codicon codicon-add" aria-hidden="true" />
        </button>
      </div>
    </footer>
  );
}
