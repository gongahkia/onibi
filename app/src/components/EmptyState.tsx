import { useMemo } from "react";
import { useSessionStore } from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

function openCommandPalette() {
  window.dispatchEvent(new CustomEvent("onibi:open-command-palette"));
}

export function EmptyState() {
  const workspaces = useSessionStore((state) => state.workspaces);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const setActiveSidebarView = useSessionStore((state) => state.setActiveSidebarView);
  const recentWorkspaces = useMemo(() => workspaces.slice(0, 5), [workspaces]);

  async function handleOpenFolder() {
    const workspace = await chooseWorkspaceFolder();
    if (workspace) {
      addWorkspace(workspace);
      setActiveSidebarView("files");
    }
  }

  return (
    <div className="empty-state welcome" data-testid="empty-state">
      <div className="welcome-content">
        <header className="welcome-header">
          <div className="welcome-logo" aria-hidden="true">
            <i className="codicon codicon-flame" />
          </div>
          <h1 className="welcome-title">Onibi</h1>
          <p className="welcome-subtitle">
            Local AI agent cockpit. Open a folder, start a session.
          </p>
        </header>
        <div className="welcome-grid">
          <section className="welcome-section">
            <h2>Start</h2>
            <button type="button" className="welcome-link" onClick={openCommandPalette}>
              <i className="codicon codicon-add" aria-hidden="true" />
              <span>New Session...</span>
            </button>
            <button type="button" className="welcome-link" onClick={handleOpenFolder}>
              <i className="codicon codicon-folder-opened" aria-hidden="true" />
              <span>Open Folder...</span>
            </button>
            <button type="button" className="welcome-link" onClick={openCommandPalette}>
              <i className="codicon codicon-source-control" aria-hidden="true" />
              <span>Clone Git Repository...</span>
            </button>
          </section>
          <section className="welcome-section">
            <h2>Recent</h2>
            {recentWorkspaces.length === 0 ? (
              <p className="welcome-muted">No recent folders</p>
            ) : (
              recentWorkspaces.map((workspace) => (
                <button
                  key={workspace.id}
                  type="button"
                  className="welcome-link"
                  onClick={() => setActiveSidebarView("files")}
                  title={workspace.path}
                >
                  <i className="codicon codicon-folder" aria-hidden="true" />
                  <span>{workspace.name}</span>
                  <span className="welcome-muted welcome-path">{workspace.path}</span>
                </button>
              ))
            )}
          </section>
          <section className="welcome-section">
            <h2>Help</h2>
            <button type="button" className="welcome-link" onClick={openCommandPalette}>
              <i className="codicon codicon-command-center" aria-hidden="true" />
              <span>Show All Commands</span>
              <span className="welcome-muted welcome-shortcut">⌘P</span>
            </button>
            <a
              className="welcome-link"
              href="https://onibi.sh"
              target="_blank"
              rel="noreferrer"
            >
              <i className="codicon codicon-book" aria-hidden="true" />
              <span>Documentation</span>
            </a>
            <a
              className="welcome-link"
              href="https://github.com/gongahkia/onibi"
              target="_blank"
              rel="noreferrer"
            >
              <i className="codicon codicon-github" aria-hidden="true" />
              <span>GitHub Repository</span>
            </a>
          </section>
        </div>
      </div>
    </div>
  );
}
