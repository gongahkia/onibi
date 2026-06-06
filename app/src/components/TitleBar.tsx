import { useEffect, useMemo, useState } from "react";
import { agentDisplayLabel, useSessionStore } from "../lib/sessions";

function basename(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

function selectedTitle(selection: ReturnType<typeof useSessionStore.getState>["selectedFile"]): string | null {
  if (!selection) return null;
  if (selection.type === "file") return selection.name || basename(selection.path);
  if (selection.type === "git-diff") return `${selection.name} (diff)`;
  if (selection.type === "agent-review") return `${selection.name} (review)`;
  if (selection.type === "web") return selection.name || selection.url;
  return null;
}

export function TitleBar() {
  const [approvalPulse, setApprovalPulse] = useState(false);
  const selection = useSessionStore((state) => state.selectedFile);
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const agentLabelOverrides = useSessionStore((state) => state.settings.agentLabelOverrides);

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

  const fileTitle = selectedTitle(selection);
  const workspaceTitle = activeWorkspace?.name ?? null;
  const agentLabel = activeSession
    ? agentDisplayLabel(activeSession.agent, agentLabelOverrides)
    : null;
  const showAgent = agentLabel && !selection; // terminal active, no file open
  const composed = showAgent && workspaceTitle
    ? `${agentLabel} · ${workspaceTitle}`
    : fileTitle && workspaceTitle
      ? `${fileTitle} — ${workspaceTitle}`
      : fileTitle ?? workspaceTitle ?? "Onibi";

  useEffect(() => {
    document.title = composed;
  }, [composed]);

  useEffect(() => {
    if (!activeSession?.id) {
      return;
    }
    let timer: number | null = null;
    function handleApprovalAttention(event: Event) {
      const detail = (event as CustomEvent<{
        escalate?: boolean;
        sessionId?: string | null;
      }>).detail;
      if (!detail?.escalate || detail.sessionId !== activeSession.id) {
        return;
      }
      setApprovalPulse(true);
      if (timer !== null) {
        window.clearTimeout(timer);
      }
      timer = window.setTimeout(() => {
        setApprovalPulse(false);
        timer = null;
      }, 3200);
    }
    window.addEventListener("onibi:approval-attention", handleApprovalAttention);
    return () => {
      window.removeEventListener("onibi:approval-attention", handleApprovalAttention);
      if (timer !== null) {
        window.clearTimeout(timer);
      }
    };
  }, [activeSession?.id]);

  const platformKey = typeof navigator !== "undefined" && /Mac|iPhone|iPod|iPad/.test(navigator.platform)
    ? "⌘K"
    : "Ctrl+K";

  function openPalette() {
    window.dispatchEvent(new CustomEvent("onibi:open-command-palette"));
  }

  return (
    <div className="title-bar" role="banner">
      <div className="title-bar-spacer" />
      <div className="title-bar-center" title={composed}>
        {showAgent && activeSession ? (
          <span
            className={`title-agent-status ${activeSession.status} ${
              approvalPulse ? "approval-attention" : ""
            }`}
            aria-label={activeSession.status}
          />
        ) : null}
        {composed}
      </div>
      <div className="title-bar-actions">
        <button
          type="button"
          className="title-cmdk-pill"
          onClick={openPalette}
          title="Open command palette"
          aria-label="Open command palette"
        >
          <i className="codicon codicon-search" aria-hidden="true" />
          <span className="title-cmdk-text">Search commands…</span>
          <kbd className="title-cmdk-kbd">{platformKey}</kbd>
        </button>
      </div>
    </div>
  );
}
