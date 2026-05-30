import { useMemo, useState } from "react";
import {
  useSessionStore,
  type WorkspaceSidebarView,
} from "../lib/sessions";
import { ActivityCenter } from "./ActivityCenter";
import { SettingsPane } from "./SettingsPane";

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
  const [settingsOpen, setSettingsOpen] = useState(false);
  const view = useSessionStore((state) => state.activeSidebarView);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const sessions = useSessionStore((state) => state.sessions);
  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, s) => sum + s.pendingApprovals.length, 0),
    [sessions],
  );
  const sessionsBadge = sessions.length || undefined;

  const topItems: ActivityItem[] = [
    { id: "files", label: "Explorer", icon: "codicon-files" },
    { id: "search", label: "Search", icon: "codicon-search" },
    { id: "source-control", label: "Source Control", icon: "codicon-source-control" },
    { id: "sessions", label: "Sessions", icon: "codicon-terminal", badge: sessionsBadge },
    { id: "approvals", label: "Approvals", icon: "codicon-bell", badge: pendingApprovals || undefined },
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
      </div>
      <div className="activity-bar-group bottom">
        <button
          type="button"
          className="activity-bar-item"
          aria-label="Activity center"
          title="Activity"
          onClick={() => setActivityOpen(true)}
        >
          <i className="codicon codicon-pulse" aria-hidden="true" />
        </button>
        <button
          type="button"
          className="activity-bar-item"
          aria-label="Settings"
          title="Manage"
          onClick={() => setSettingsOpen(true)}
        >
          <i className="codicon codicon-settings-gear" aria-hidden="true" />
        </button>
      </div>
      <ActivityCenter open={activityOpen} onClose={() => setActivityOpen(false)} />
      <SettingsPane open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </nav>
  );
}
