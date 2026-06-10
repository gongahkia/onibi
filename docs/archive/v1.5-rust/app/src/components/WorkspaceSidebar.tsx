import { useCallback, useEffect, useMemo, useState } from "react";
import { getGitStatus, type GitStatus } from "../lib/git";
import {
  useSessionStore,
  type Session,
  type Workspace,
  type WorkspaceSidebarView,
} from "../lib/sessions";
import { RecentFilesList } from "./RecentFilesList";
import { SourceControlView } from "./SourceControlView";
import { WorkspaceSearchView } from "./WorkspaceSearchView";

function activeWorkspaceFor(
  sessions: Session[],
  workspaces: Workspace[],
  activeWorkspaceId: string | null,
  activeSessionId: string | null,
): Workspace | null {
  const activeWorkspace = workspaces.find(
    (workspace) => workspace.id === activeWorkspaceId,
  );
  if (activeWorkspace) {
    return activeWorkspace;
  }
  const activeSession = sessions.find((session) => session.id === activeSessionId);
  return (
    workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ?? null
  );
}

const VIEW_TITLES: Record<WorkspaceSidebarView, string> = {
  files: "Recent Files",
  search: "Search",
  "source-control": "Source Control",
};

export function WorkspaceSidebarContent({ view }: { view: WorkspaceSidebarView }) {
  const {
    activeWorkspace,
    gitError,
    gitLoading,
    gitStatus,
    refreshGitStatus,
  } = useWorkspaceSidebarData();

  return (
    <>
      <header className="sidebar-section-header">
        <span className="sidebar-section-title">{VIEW_TITLES[view] ?? "Recent Files"}</span>
      </header>
      <div className="workspace-view-pane">
        {view === "files" ? (
          <RecentFilesList />
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
        ) : null}
      </div>
    </>
  );
}

function useWorkspaceSidebarData() {
  const [gitStatus, setGitStatus] = useState<GitStatus | null>(null);
  const [gitLoading, setGitLoading] = useState(false);
  const [gitError, setGitError] = useState("");
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeWorkspaceId = useSessionStore((state) => state.activeWorkspaceId);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const activeWorkspace = useMemo(
    () => activeWorkspaceFor(sessions, workspaces, activeWorkspaceId, activeSessionId),
    [activeSessionId, activeWorkspaceId, sessions, workspaces],
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

  useEffect(() => {
    void refreshGitStatus();
  }, [refreshGitStatus]);

  return {
    activeWorkspace,
    gitError,
    gitLoading,
    gitStatus,
    refreshGitStatus,
  };
}
