import { useCallback, useEffect, useMemo, useState } from "react";
import { getGitStatus, gitStateByFullPath, type GitStatus } from "../lib/git";
import { useSessionStore, type Session, type Workspace } from "../lib/sessions";
import { FileTree } from "./FileTree";
import { SourceControlView } from "./SourceControlView";

type WorkspaceSidebarView = "files" | "source-control";

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

  useEffect(() => {
    void refreshGitStatus();
  }, [refreshGitStatus]);

  const gitStatusByPath = useMemo(() => gitStateByFullPath(gitStatus), [gitStatus]);
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
      </div>
      <div className="workspace-view-pane">
        {view === "files" ? (
          <FileTree gitStatusByPath={gitStatusByPath} />
        ) : (
          <SourceControlView
            workspace={activeWorkspace}
            status={gitStatus}
            loading={gitLoading}
            error={gitError}
            onRefresh={refreshGitStatus}
          />
        )}
      </div>
    </div>
  );
}
