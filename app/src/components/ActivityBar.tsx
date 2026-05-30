import { useMemo, useState } from "react";
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
  const [activityOpen, setActivityOpen] = useState(false);
  const view = useSessionStore((state) => state.activeSidebarView);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const workspaces = useSessionStore((state) => state.workspaces);

  const topItems: ActivityItem[] = [
    { id: "files", label: "Explorer", icon: "codicon-files" },
    { id: "search", label: "Search", icon: "codicon-search" },
    { id: "source-control", label: "Source Control", icon: "codicon-source-control" },
  ];

  function handleSelect(id: WorkspaceSidebarView) {
    if (view === id && !sidebarCollapsed) {
      onToggleSidebar();
      return;
    }
    if (sidebarCollapsed) {
      onToggleSidebar();
    }
    setView(id);
  }

  const activeWorkspaceId = useMemo(() => {
    const activeSession = activeSessionId
      ? sessions.find((session) => session.id === activeSessionId)
      : null;
    return activeSession?.workspaceId ?? selectedFile?.workspaceId ?? workspaces[0]?.id ?? null;
  }, [activeSessionId, selectedFile?.workspaceId, sessions, workspaces]);

  return (
    <nav className="activity-bar" aria-label="Activity bar">
      <div className="activity-bar-group">
        {topItems.map((item) => (
          <button
            key={item.id}
            type="button"
            className={`activity-bar-item ${view === item.id && !sidebarCollapsed ? "active" : ""}`}
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
