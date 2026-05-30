import { useEffect, useMemo, useState, type MouseEvent } from "react";
import {
  AGENT_LABELS,
  sessionAttentionState,
  sessionNeedsAttention,
  useSessionStore,
  type DiffViewMode,
  type Session,
  type Workspace,
} from "../lib/sessions";
import {
  getGitStatus,
  hasStagedChange,
  hasWorkingTreeChange,
  type GitStatusEntry,
} from "../lib/git";
import { agentIconUrl } from "../lib/agent-icons";
import { ActivityCenter } from "./ActivityCenter";
import { NewSessionDialog } from "./NewSessionDialog";
import { SettingsPane } from "./SettingsPane";

export interface AgentTabBarProps {
  orientation: "vertical" | "horizontal";
  onOpenApprovals?: () => void;
}

interface WorkflowContextMenuState {
  x: number;
  y: number;
  sessionId: string;
  loading: boolean;
  error: string;
}

function basename(path: string): string {
  return path.split("/").pop() ?? path;
}

function chooseDiffEntry(
  entries: GitStatusEntry[],
): { entry: GitStatusEntry; stage: "staged" | "working" } | null {
  const workingEntry = entries.find((entry) => hasWorkingTreeChange(entry));
  if (workingEntry) {
    return { entry: workingEntry, stage: "working" };
  }

  const stagedEntry = entries.find((entry) => hasStagedChange(entry));
  if (stagedEntry) {
    return { entry: stagedEntry, stage: "staged" };
  }

  return null;
}

export function AgentTabBar({ orientation, onOpenApprovals }: AgentTabBarProps) {
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const [activityOpen, setActivityOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [contextMenu, setContextMenu] = useState<WorkflowContextMenuState | null>(
    null,
  );
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const diffViewMode = useSessionStore((state) => state.settings.diffViewMode);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const setActiveSidebarView = useSessionStore((state) => state.setActiveSidebarView);
  const removeSession = useSessionStore((state) => state.removeSession);
  const selectFile = useSessionStore((state) => state.selectFile);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, session) => sum + session.pendingApprovals.length, 0),
    [sessions],
  );
  const workspaceById = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace])),
    [workspaces],
  );
  const contextSession = contextMenu
    ? sessions.find((session) => session.id === contextMenu.sessionId) ?? null
    : null;
  const contextWorkspace = contextSession
    ? workspaceById.get(contextSession.workspaceId) ?? null
    : null;

  useEffect(() => {
    if (!contextMenu) {
      return;
    }

    function closeContextMenu() {
      setContextMenu(null);
    }

    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        closeContextMenu();
      }
    }

    window.addEventListener("click", closeContextMenu);
    window.addEventListener("scroll", closeContextMenu, true);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("click", closeContextMenu);
      window.removeEventListener("scroll", closeContextMenu, true);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [contextMenu]);

  function openContextMenu(event: MouseEvent<HTMLButtonElement>, sessionId: string) {
    event.preventDefault();
    event.stopPropagation();
    setActiveSession(sessionId);
    setContextMenu({
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - 280)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - 360)),
      sessionId,
      loading: false,
      error: "",
    });
  }

  function setMenuError(sessionId: string, message: string) {
    setContextMenu((current) =>
      current?.sessionId === sessionId
        ? { ...current, loading: false, error: message }
        : current,
    );
  }

  async function openWorkspaceDiff(session: Session, workspace: Workspace | null) {
    setActiveSession(session.id);
    if (!workspace) {
      setMenuError(session.id, "This workflow has no workspace.");
      return;
    }

    setContextMenu((current) =>
      current?.sessionId === session.id
        ? { ...current, loading: true, error: "" }
        : current,
    );

    try {
      const status = await getGitStatus(workspace.path);
      if (!status.isRepo) {
        setMenuError(session.id, "No Git repository in this workspace.");
        return;
      }

      const diff = chooseDiffEntry(status.entries);
      if (!diff) {
        setMenuError(session.id, "No Git changes in this workspace.");
        return;
      }

      selectFile({
        type: "git-diff",
        workspaceId: workspace.id,
        workspaceRoot: workspace.path,
        path: diff.entry.path,
        name: basename(diff.entry.path),
        stage: diff.stage,
      });
      setContextMenu(null);
    } catch (caught) {
      setMenuError(
        session.id,
        caught instanceof Error ? caught.message : String(caught),
      );
    }
  }

  return (
    <nav className={`agent-tab-bar ${orientation}`} aria-label="Agent sessions">
      <div className="agent-tabs">
        {sessions.map((session) => {
          const workspace = workspaceById.get(session.workspaceId);
          const attention = sessionAttentionState(session);
          const flash = sessionNeedsAttention(session);
          return (
            <button
              key={session.id}
              type="button"
              className={`tab ${activeSessionId === session.id ? "active" : ""} ${
                flash ? "flash" : ""
              }`}
              title={`${AGENT_LABELS[session.agent]} · ${workspace?.name ?? "Workspace"}`}
              aria-label={`${AGENT_LABELS[session.agent]} session`}
              onClick={() => setActiveSession(session.id)}
              onContextMenu={(event) => openContextMenu(event, session.id)}
            >
              <img src={agentIconUrl(session.agent)} alt="" />
              <span className={`tab-status ${session.status}`} />
              {flash ? (
                <span
                  className={`tab-attention attention-${attention}`}
                  aria-label={attention}
                />
              ) : null}
              {session.preview ? (
                <span className="tab-preview" title={session.preview.url}>
                  {session.preview.label}
                </span>
              ) : null}
            </button>
          );
        })}
      </div>
      <div className="agent-actions">
        <button
          type="button"
          className="icon-button"
          aria-label="New session"
          title="New session"
          onClick={() => setNewSessionOpen(true)}
        >
          +
        </button>
        <button
          type="button"
          className="icon-button"
          aria-label="Approvals"
          title="Approvals"
          onClick={() => {
            if (onOpenApprovals) {
              onOpenApprovals();
            } else {
              setActiveSidebarView("approvals");
            }
          }}
        >
          <i className="codicon codicon-bell" aria-hidden="true" />
          {pendingApprovals > 0 ? (
            <span className="agent-action-badge" aria-label={`${pendingApprovals} approvals`}>
              {pendingApprovals > 99 ? "99+" : pendingApprovals}
            </span>
          ) : null}
        </button>
        <button
          type="button"
          className="icon-button activity-button"
          aria-label="Activity"
          title="Activity"
          onClick={() => setActivityOpen(true)}
        >
          <svg aria-hidden="true" viewBox="0 0 24 24" focusable="false">
            <path d="M12 7v5l3 2" />
            <path d="M5.6 5.9A8.5 8.5 0 1 1 4 12" />
            <path d="M4 4v5h5" />
          </svg>
        </button>
        <button
          type="button"
          className="icon-button"
          aria-label="Settings"
          title="Settings"
          onClick={() => setSettingsOpen(true)}
        >
          ⚙
        </button>
      </div>
      <NewSessionDialog
        open={newSessionOpen}
        onClose={() => setNewSessionOpen(false)}
      />
      <ActivityCenter open={activityOpen} onClose={() => setActivityOpen(false)} />
      <SettingsPane open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      {contextMenu && contextSession ? (
        <WorkflowContextMenu
          menu={contextMenu}
          session={contextSession}
          workspace={contextWorkspace}
          diffViewMode={diffViewMode}
          onClose={() => setContextMenu(null)}
          onSwitch={() => setActiveSession(contextSession.id)}
          onOpenDiff={() => void openWorkspaceDiff(contextSession, contextWorkspace)}
          onCopySessionId={() =>
            void navigator.clipboard?.writeText(contextSession.id)
          }
          onCopyWorkspacePath={() =>
            contextWorkspace
              ? void navigator.clipboard?.writeText(contextWorkspace.path)
              : undefined
          }
          onDiffViewMode={(mode) => updateSettings({ diffViewMode: mode })}
          onCloseSession={() => removeSession(contextSession.id)}
        />
      ) : null}
    </nav>
  );
}

interface WorkflowContextMenuProps {
  menu: WorkflowContextMenuState;
  session: Session;
  workspace: Workspace | null;
  diffViewMode: DiffViewMode;
  onClose: () => void;
  onSwitch: () => void;
  onOpenDiff: () => void;
  onCopySessionId: () => void;
  onCopyWorkspacePath: () => void;
  onDiffViewMode: (mode: DiffViewMode) => void;
  onCloseSession: () => void;
}

function WorkflowContextMenu({
  menu,
  session,
  workspace,
  diffViewMode,
  onClose,
  onSwitch,
  onOpenDiff,
  onCopySessionId,
  onCopyWorkspacePath,
  onDiffViewMode,
  onCloseSession,
}: WorkflowContextMenuProps) {
  function run(action: () => void) {
    action();
    onClose();
  }

  return (
    <div
      className="file-context-menu workflow-context-menu"
      role="menu"
      style={{ left: menu.x, top: menu.y }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      <div className="context-menu-title">
        {session.title || AGENT_LABELS[session.agent]}
        {workspace ? <span>{workspace.name}</span> : null}
      </div>
      <button type="button" role="menuitem" onClick={() => run(onSwitch)}>
        Focus Workflow
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={menu.loading || !workspace}
        onClick={onOpenDiff}
      >
        {menu.loading ? "Opening Diff..." : "Open Workspace Diff"}
      </button>
      <div className="context-separator" />
      <button
        type="button"
        role="menuitemradio"
        aria-checked={diffViewMode === "side-by-side"}
        disabled={diffViewMode === "side-by-side"}
        onClick={() => run(() => onDiffViewMode("side-by-side"))}
      >
        Use Side-by-side Diff
      </button>
      <button
        type="button"
        role="menuitemradio"
        aria-checked={diffViewMode === "unified"}
        disabled={diffViewMode === "unified"}
        onClick={() => run(() => onDiffViewMode("unified"))}
      >
        Use Unified Diff
      </button>
      <div className="context-separator" />
      <button type="button" role="menuitem" onClick={() => run(onCopySessionId)}>
        Copy Workflow ID
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!workspace}
        onClick={() => run(onCopyWorkspacePath)}
      >
        Copy Workspace Path
      </button>
      {menu.error ? <div className="context-menu-error">{menu.error}</div> : null}
      <div className="context-separator" />
      <button
        type="button"
        role="menuitem"
        className="danger"
        onClick={() => run(onCloseSession)}
      >
        Close Workflow
      </button>
    </div>
  );
}
