import {
  useSessionStore,
  type WorkspaceRightDockView,
} from "../lib/sessions";
import { WorkspaceSidebarContent } from "./WorkspaceSidebar";

const DOCK_ITEMS: Array<{
  id: WorkspaceRightDockView;
  label: string;
  icon: string;
}> = [
  { id: "files", label: "Recent Files", icon: "codicon-files" },
  { id: "search", label: "Search", icon: "codicon-search" },
  { id: "source-control", label: "Source Control", icon: "codicon-source-control" },
];

export function WorkspaceRightDock() {
  const view = useSessionStore((state) => state.rightDockView);
  const mode = useSessionStore((state) => state.rightDockMode);
  const setRightDockView = useSessionStore((state) => state.setRightDockView);
  const setRightDockMode = useSessionStore((state) => state.setRightDockMode);
  const expanded = mode === "expanded";

  function selectView(nextView: WorkspaceRightDockView) {
    if (view === nextView && expanded) {
      setRightDockMode("compressed");
      return;
    }
    setRightDockView(nextView);
  }

  return (
    <aside
      className={`workspace-right-dock ${expanded ? "expanded" : "compressed"}`}
      data-mode={mode}
      aria-label="Workspace dock"
    >
      <div className="workspace-right-rail" aria-label="Workspace dock controls">
        <div className="workspace-right-rail-group">
          {DOCK_ITEMS.map((item) => (
            <button
              key={item.id}
              type="button"
              className={`workspace-right-rail-button ${
                view === item.id && expanded ? "active" : ""
              }`}
              aria-label={item.label}
              title={item.label}
              aria-pressed={view === item.id && expanded}
              onClick={() => selectView(item.id)}
            >
              <i className={`codicon ${item.icon}`} aria-hidden="true" />
            </button>
          ))}
        </div>
        <button
          type="button"
          className="workspace-right-rail-button"
          aria-label={expanded ? "Compress workspace dock" : "Expand workspace dock"}
          title={expanded ? "Compress" : "Expand"}
          onClick={() => setRightDockMode(expanded ? "compressed" : "expanded")}
        >
          <i
            className={`codicon ${expanded ? "codicon-chevron-right" : "codicon-chevron-left"}`}
            aria-hidden="true"
          />
        </button>
      </div>
      {expanded ? (
        <div className="workspace-right-panel">
          <WorkspaceSidebarContent view={view} />
        </div>
      ) : null}
    </aside>
  );
}
