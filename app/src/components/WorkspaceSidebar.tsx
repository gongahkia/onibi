import { useCallback, useEffect, useMemo, useState } from "react";
import { listAgentReviews, type AgentReviewRecord } from "../lib/agent-review";
import { getGitStatus, gitStateByFullPath, type GitStatus } from "../lib/git";
import { useSessionStore, type Session, type Workspace } from "../lib/sessions";
import { FileTree } from "./FileTree";
import { SourceControlView } from "./SourceControlView";
import { WorkspaceSearchView } from "./WorkspaceSearchView";

function activeWorkspaceFor(
  sessions: Session[],
  workspaces: Workspace[],
  activeSessionId: string | null,
): Workspace | null {
  const activeSession = sessions.find((session) => session.id === activeSessionId);
  return workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ?? null;
}

export function WorkspaceSidebar() {
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [agentReviews, setAgentReviews] = useState<AgentReviewRecord[]>([]);
  const [gitLoading, setGitLoading] = useState(false);
  const [gitError, setGitError] = useState("");
  const [filesMode, setFilesMode] = useState<"tree" | "search">("tree");
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const view = useSessionStore((state) => state.activeSidebarView);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const effectiveView =
    view === "search" || view === "sessions" || view === "history" ? "files" : view;
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
    if (view === "search") {
      setFilesMode("search");
      setView("files");
    }
    if (view === "sessions" || view === "history") {
      setFilesMode("tree");
      setView("files");
    }
  }, [setView, view]);

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
          className={effectiveView === "files" ? "active" : ""}
          role="tab"
          aria-selected={effectiveView === "files"}
          onClick={() => setView("files")}
        >
          Files
        </button>
        <button
          type="button"
          className={effectiveView === "source-control" ? "active" : ""}
          role="tab"
          aria-selected={effectiveView === "source-control"}
          onClick={() => setView("source-control")}
        >
          Git
          {changeCount > 0 ? <span className="workspace-view-badge">{changeCount}</span> : null}
        </button>
      </div>
      <div className="workspace-view-pane">
        {effectiveView === "files" ? (
          <section className="files-view" aria-label="Files">
            <div className="files-view-mode" role="tablist" aria-label="File view mode">
              <button
                type="button"
                className={filesMode === "tree" ? "active" : ""}
                onClick={() => setFilesMode("tree")}
              >
                Tree
              </button>
              <button
                type="button"
                className={filesMode === "search" ? "active" : ""}
                onClick={() => setFilesMode("search")}
              >
                Text Search
              </button>
            </div>
            {filesMode === "tree" ? (
              <FileTree
                gitStatusByPath={gitStatusByPath}
                agentReviewsByPath={agentReviewsByPath}
              />
            ) : (
              <WorkspaceSearchView />
            )}
          </section>
        ) : effectiveView === "source-control" ? (
          <SourceControlView
            workspace={activeWorkspace}
            status={gitStatus}
            loading={gitLoading}
            error={gitError}
            onRefresh={refreshGitStatus}
          />
        ) : null}
      </div>
    </div>
  );
}
