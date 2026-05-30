import { useCallback, useEffect, useMemo, useState } from "react";
import { listAgentReviews, type AgentReviewRecord } from "../lib/agent-review";
import { getGitStatus, gitStateByFullPath, type GitStatus } from "../lib/git";
import {
  useSessionStore,
  type Session,
  type Workspace,
  type WorkspaceSidebarView,
} from "../lib/sessions";
import { AgentTabBar } from "./AgentTabBar";
import { FileTree } from "./FileTree";
import { SessionListView } from "./SessionListView";
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

const VIEW_TITLES: Record<WorkspaceSidebarView, string> = {
  files: "Explorer",
  search: "Search",
  "source-control": "Source Control",
  sessions: "Sessions",
  approvals: "Approvals",
};

export function WorkspaceSidebar() {
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [agentReviews, setAgentReviews] = useState<AgentReviewRecord[]>([]);
  const [gitLoading, setGitLoading] = useState(false);
  const [gitError, setGitError] = useState("");
  const [overflowOpen, setOverflowOpen] = useState(false);
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const view = useSessionStore((state) => state.activeSidebarView);
  const showHiddenFiles = useSessionStore((state) => state.settings.showHiddenFiles);
  const updateSettings = useSessionStore((state) => state.updateSettings);
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

  const pendingApprovals = useMemo(() => {
    return sessions.filter((s) => s.pendingApprovals.length > 0);
  }, [sessions]);

  return (
    <aside className="workspace-sidebar">
      <header className="sidebar-section-header">
        <span className="sidebar-section-title">{VIEW_TITLES[view] ?? "Explorer"}</span>
        <div className="sidebar-section-actions">
          {view === "files" ? (
            <button
              type="button"
              className="sidebar-icon-button"
              aria-label="More actions"
              title="Views and More Actions..."
              onClick={() => setOverflowOpen((v) => !v)}
            >
              <i className="codicon codicon-ellipsis" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      </header>
      {overflowOpen && view === "files" ? (
        <div className="sidebar-overflow-menu" role="menu">
          <button
            type="button"
            role="menuitemcheckbox"
            aria-checked={showHiddenFiles}
            onClick={() => {
              updateSettings({ showHiddenFiles: !showHiddenFiles });
              setOverflowOpen(false);
            }}
          >
            <i
              className={`codicon ${showHiddenFiles ? "codicon-check" : "codicon-blank"}`}
              aria-hidden="true"
            />
            Show Hidden Files
          </button>
        </div>
      ) : null}
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
          <div className="sidebar-sessions">
            <AgentTabBar orientation="vertical" />
          </div>
        ) : view === "approvals" ? (
          <ApprovalsView pendingSessions={pendingApprovals} />
        ) : null}
      </div>
    </aside>
  );
}

interface ApprovalsViewProps {
  pendingSessions: Session[];
}

function ApprovalsView({ pendingSessions }: ApprovalsViewProps) {
  if (pendingSessions.length === 0) {
    return (
      <div className="sidebar-empty">
        <i className="codicon codicon-bell-slash" aria-hidden="true" />
        <p>No pending approvals</p>
        <span>You'll see agent tool calls awaiting your decision here.</span>
      </div>
    );
  }
  return <SessionListView />;
}
