import { useEffect, useMemo, useRef, useState } from "react";
import { getGitStatus, type GitStatus } from "../lib/git";
import { useSessionStore } from "../lib/sessions";

function basename(path: string | undefined | null): string {
  if (!path) {
    return "";
  }
  return path.split("/").filter(Boolean).pop() ?? path;
}

export function FocusHud() {
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selection = useSessionStore((state) => state.selectedFile);
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [visible, setVisible] = useState(false);
  const initializedRef = useRef(false);

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [activeSessionId, sessions],
  );
  const activeWorkspace = useMemo(() => {
    if (selection && "workspaceId" in selection) {
      return workspaces.find((workspace) => workspace.id === selection.workspaceId) ?? null;
    }
    return workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ?? null;
  }, [activeSession?.workspaceId, selection, workspaces]);

  useEffect(() => {
    let cancelled = false;
    if (!activeWorkspace) {
      setGitStatus(null);
      return;
    }
    void getGitStatus(activeWorkspace.path)
      .then((status) => {
        if (!cancelled) {
          setGitStatus(status);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setGitStatus(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [activeWorkspace?.path]);

  const branchLabel = gitStatus?.isRepo && gitStatus.branch
    ? `${gitStatus.branch}${gitStatus.ahead ? ` ↑${gitStatus.ahead}` : ""}${gitStatus.behind ? ` ↓${gitStatus.behind}` : ""}`
    : null;
  const gitChangeCount = gitStatus?.isRepo ? gitStatus.entries.length : 0;
  const cwdLabel = basename(activeSession?.cwd);
  const exitCode = activeSession?.lastExitCode;
  const contextKey = [
    activeSession?.id ?? "",
    activeWorkspace?.id ?? "",
    cwdLabel,
    typeof exitCode === "number" ? String(exitCode) : "",
    branchLabel ?? "",
  ].join("|");

  useEffect(() => {
    if (!contextKey.replaceAll("|", "")) {
      return;
    }
    if (!initializedRef.current) {
      initializedRef.current = true;
      return;
    }
    setVisible(true);
    const timer = window.setTimeout(() => setVisible(false), 3000);
    return () => window.clearTimeout(timer);
  }, [contextKey]);

  if (!visible) {
    return null;
  }

  return (
    <div className="focus-hud" role="status" aria-live="polite">
      {branchLabel ? (
        <span title="Git branch">
          <i className="codicon codicon-source-control" aria-hidden="true" />
          {branchLabel}
          {gitChangeCount > 0 ? ` ${gitChangeCount}` : ""}
        </span>
      ) : null}
      {cwdLabel ? (
        <span title={activeSession?.cwd ?? "Current directory"}>
          <i className="codicon codicon-folder" aria-hidden="true" />
          {cwdLabel}
        </span>
      ) : null}
      {typeof exitCode === "number" ? (
        <span title={`Last exit code ${exitCode}`}>
          <i className={`codicon ${exitCode === 0 ? "codicon-pass" : "codicon-error"}`} aria-hidden="true" />
          {exitCode === 0 ? "ok" : `exit ${exitCode}`}
        </span>
      ) : null}
    </div>
  );
}
