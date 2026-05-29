import { useCallback, useEffect, useMemo, useState } from "react";
import { listAgentReviews, type AgentReviewRecord } from "../lib/agent-review";
import { getGitStatus, gitStateByFullPath, type GitStatus } from "../lib/git";
import { useSessionStore, type Session, type Workspace } from "../lib/sessions";
import { FileTree } from "./FileTree";
import { SessionHistoryView } from "./SessionHistoryView";
import { SessionListView } from "./SessionListView";
import { SourceControlView } from "./SourceControlView";
import { WorkspaceSearchView } from "./WorkspaceSearchView";

type WorkspaceSidebarView = "files" | "search" | "source-control" | "sessions" | "history";

function activeWorkspaceFor(
  sessions: Session[],
  workspaces: Workspace[],
  activeSessionId: string | null,
): Workspace | null {
  const activeSession = sessions.find((session) => session.id === activeSessionId);
  return workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ?? null;
}

export function WorkspaceSidebar() {
  const [view, setView] = useState<WorkspaceSidebarView>("files");
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [agentReviews, setAgentReviews] = useState<AgentReviewRecord[]>([]);
  const [gitLoading, setGitLoading] = useState(false);
  const [gitError, setGitError] = useState("");
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const activeWorkspace = useMemo(
    () => activeWorkspaceFor(sessions, workspaces, activeSessionId),
    [activeSessionId, sessions, workspaces],
  );

  const refreshGitStatus = useCallback(async () => {
    if (!activeWorkspace) {
      setGitStatus(null);
      setGitError("");
      return;
    }
    setGitLoading(true);
    setGitError("");
    try {
      setGitStatus(await getGitStatus(activeWorkspace.path));
    } catch (caught) {
      setGitStatus(null);
      setGitError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setGitLoading(false);
    }
  }, [activeWorkspace]);

  const refreshAgentReviews = useCallback(async () => {
    if (!activeWorkspace) {
      setAgentReviews([]);
      return;
    }
    setAgentReviews(await listAgentReviews(activeWorkspace.path).catch(() => []));
  }, [activeWorkspace]);

  useEffect(() => {
    void refreshGitStatus();
  }, [refreshGitStatus]);

  useEffect(() => {
    void refreshAgentReviews();
    const timer = window.setInterval(() => {
      void refreshAgentReviews();
    }, 2000);
    return () => window.clearInterval(timer);
  }, [refreshAgentReviews]);

  const gitStatusByPath = useMemo(() => gitStateByFullPath(gitStatus), [gitStatus]);
  const agentReviewsByPath = useMemo(
    () => Object.fromEntries(agentReviews.map((record) => [record.fullPath, record])),
    [agentReviews],
  );
  const changeCount = gitStatus?.entries.length ?? 0;

  return (
    <div className="workspace-sidebar">
      <div className="workspace-view-switcher" role="tablist" aria-label="Workspace views">
        <button
          type="button"
          className={view === "files" ? "active" : ""}
          role="tab"
          aria-selected={view === "files"}
          onClick={() => setView("files")}
        >
          Files
        </button>
        <button
          type="button"
          className={view === "source-control" ? "active" : ""}
          role="tab"
          aria-selected={view === "source-control"}
          onClick={() => setView("source-control")}
        >
          Git
          {changeCount > 0 ? <span className="workspace-view-badge">{changeCount}</span> : null}
        </button>
        <button
          type="button"
          className={view === "search" ? "active" : ""}
          role="tab"
          aria-selected={view === "search"}
          onClick={() => setView("search")}
        >
          Search
        </button>
        <button
          type="button"
          className={view === "sessions" ? "active" : ""}
          role="tab"
          aria-selected={view === "sessions"}
          onClick={() => setView("sessions")}
        >
          Sessions
        </button>
        <button
          type="button"
          className={view === "history" ? "active" : ""}
          role="tab"
          aria-selected={view === "history"}
          onClick={() => setView("history")}
        >
          History
        </button>
      </div>
      <div className="workspace-view-pane">
        {view === "files" ? (
          <FileTree
            gitStatusByPath={gitStatusByPath}
            agentReviewsByPath={agentReviewsByPath}
          />
        ) : view === "search" ? (
          <WorkspaceSearchView />
        ) : view === "source-control" ? (
          <SourceControlView
            workspace={activeWorkspace}
            status={gitStatus}
            loading={gitLoading}
            error={gitError}
            onRefresh={refreshGitStatus}
          />
        ) : view === "sessions" ? (
          <SessionListView />
        ) : (
          <SessionHistoryView />
        )}
      </div>
    </div>
  );
}
