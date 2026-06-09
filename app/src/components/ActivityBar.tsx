import { useEffect, useMemo, useState } from "react";
import {
  useSessionStore,
  type WorkspaceSidebarView,
} from "../lib/sessions";
import { ActivityCenter } from "./ActivityCenter";

interface ActivityItem {
  id: WorkspaceSidebarView;
  label: string;
  icon: string; // codicon class name
  badge?: number;
}

interface ActivityBarProps {
  sidebarCollapsed: boolean;
  onToggleSidebar: () => void;
}

export function ActivityBar({ sidebarCollapsed, onToggleSidebar }: ActivityBarProps) {
  const [approvalAttention, setApprovalAttention] = useState(false);
  const activityOpen = useSessionStore((state) => state.activityCenterOpen);
  const setActivityOpen = useSessionStore((state) => state.setActivityCenterOpen);
  const view = useSessionStore((state) => state.activeSidebarView);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const rightDockView = useSessionStore((state) => state.rightDockView);
  const rightDockMode = useSessionStore((state) => state.rightDockMode);
  const setRightDockView = useSessionStore((state) => state.setRightDockView);
  const setRightDockMode = useSessionStore((state) => state.setRightDockMode);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const workspaces = useSessionStore((state) => state.workspaces);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const selectFile = useSessionStore((state) => state.selectFile);

  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, s) => sum + s.pendingApprovals.length, 0),
    [sessions],
  );

  const topItems: ActivityItem[] = [
    { id: "files", label: "Explorer", icon: "codicon-files" },
    { id: "search", label: "Search", icon: "codicon-search" },
    { id: "source-control", label: "Source Control", icon: "codicon-source-control" },
    {
      id: "approvals",
      label: "Approvals",
      icon: pendingApprovals > 0 ? "codicon-bell-dot" : "codicon-bell",
      badge: pendingApprovals > 0 ? pendingApprovals : undefined,
    },
  ];

  useEffect(() => {
    let timer: number | null = null;
    function handleApprovalAttention() {
      setApprovalAttention(true);
      if (timer !== null) {
        window.clearTimeout(timer);
      }
      timer = window.setTimeout(() => {
        setApprovalAttention(false);
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
  }, []);

  function handleSelect(id: WorkspaceSidebarView) {
    if (id === "files" || id === "search") {
      if (rightDockView === id && rightDockMode === "expanded") {
        setRightDockMode("compressed");
      } else {
        setRightDockView(id);
      }
      return;
    }
    if (view === id && !sidebarCollapsed) {
      onToggleSidebar();
      return;
    }
    if (sidebarCollapsed) {
      onToggleSidebar();
    }
    setRightDockMode("compressed");
    setView(id);
  }

  const activeSession = useMemo(
    () =>
      activeSessionId
        ? sessions.find((session) => session.id === activeSessionId)
        : null,
    [activeSessionId, sessions],
  );
  const activeWorkspaceId = useMemo(() => {
    return activeSession?.workspaceId ?? selectedFile?.workspaceId ?? workspaces[0]?.id ?? null;
  }, [activeSession?.workspaceId, selectedFile?.workspaceId, workspaces]);
  const terminalActive = selectedFile === null && Boolean(activeSessionId);

  function handleTerminalSelect() {
    const target =
      activeSession ??
      sessions.find((session) => session.workspaceId === activeWorkspaceId) ??
      sessions[0] ??
      null;
    if (target) {
      setActiveSession(target.id);
    } else {
      selectFile(null);
    }
  }

  return (
    <nav className="activity-bar" aria-label="Activity bar">
      <div className="activity-bar-group">
        <button
          type="button"
          className={`activity-bar-item ${terminalActive ? "active" : ""}`}
          aria-label="Terminal"
          title="Terminal"
          onClick={handleTerminalSelect}
        >
          <i className="codicon codicon-terminal" aria-hidden="true" />
        </button>
        {topItems.map((item) => (
          item.id === "files" || item.id === "search" ? (
            <button
              key={item.id}
              type="button"
              className={`activity-bar-item ${
                rightDockView === item.id && rightDockMode === "expanded"
                  ? "active"
                  : ""
              }`}
              aria-label={item.label}
              title={item.label}
              onClick={() => handleSelect(item.id)}
            >
              <i className={`codicon ${item.icon}`} aria-hidden="true" />
            </button>
          ) : (
          <button
            key={item.id}
            type="button"
            className={`activity-bar-item ${
              view === item.id && !sidebarCollapsed && !terminalActive ? "active" : ""
            } ${
              item.id === "approvals" && approvalAttention
                ? "approval-attention"
                : ""
            }`}
            aria-label={item.label}
            title={item.label}
            onClick={() => handleSelect(item.id)}
          >
            <i className={`codicon ${item.icon}`} aria-hidden="true" />
            {item.badge ? (
              <span className="activity-bar-badge" aria-label={`${item.badge} ${item.label}`}>
                {item.badge > 99 ? "99+" : item.badge}
              </span>
            ) : null}
          </button>
          )
        ))}
        <button
          type="button"
          className="activity-bar-item"
          aria-label="Activity center"
          title="Activity"
          onClick={() => setActivityOpen(true)}
        >
          <svg aria-hidden="true" viewBox="0 0 24 24" focusable="false">
            <path d="M12 7v5l3 2" />
            <path d="M5.6 5.9A8.5 8.5 0 1 1 4 12" />
            <path d="M4 4v5h5" />
          </svg>
        </button>
      </div>
      <ActivityCenter
        open={activityOpen}
        onClose={() => setActivityOpen(false)}
        workspaceId={activeWorkspaceId}
      />
    </nav>
  );
}
