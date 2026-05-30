import { useCallback, useEffect, useMemo, useState } from "react";
import { getGitStatus, type GitStatus } from "../lib/git";
import { useSessionStore } from "../lib/sessions";

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
        {cwdLabel ? (
          <span className="status-bar-item" title={activeSession?.cwd ?? ""}>
            <i className="codicon codicon-folder" aria-hidden="true" />
            <span>{cwdLabel}</span>
          </span>
        ) : null}
        {typeof exitCode === "number" ? (
          <span
            className={`status-bar-item ${exitCode === 0 ? "ok" : "error"}`}
            title="Last exit code"
          >
            <i className={`codicon ${exitCode === 0 ? "codicon-pass" : "codicon-error"}`} aria-hidden="true" />
            <span>{exitCode}</span>
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
