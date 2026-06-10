import { useMemo } from "react";
import { useTransportStatusQuery } from "../lib/queries";
import { useSessionStore } from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

function openCommandPalette() {
  window.dispatchEvent(new CustomEvent("onibi:open-command-palette"));
}

export function EmptyState() {
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const setActiveWorkspace = useSessionStore((state) => state.setActiveWorkspace);
  const setRightDockView = useSessionStore((state) => state.setRightDockView);
  const recentWorkspaces = useMemo(() => workspaces.slice(0, 5), [workspaces]);

  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, s) => sum + s.pendingApprovals.length, 0),
    [sessions],
  );
  const activeSessions = sessions.length;

  const { data: transports = [] } = useTransportStatusQuery({
    refetchInterval: 30_000,
  });
  const runningTransport = transports.find((t) => t.enabled && t.status.state === "running");

  async function handleOpenFolder() {
    const workspace = await chooseWorkspaceFolder();
    if (workspace) {
      addWorkspace(workspace);
      setActiveWorkspace(workspace.id);
      setRightDockView("files");
    }
  }

  return (
    <div className="empty-state welcome" data-testid="empty-state">
      <div className="welcome-content">
        <div className="welcome-hero">
          <div className="welcome-hero-title">Onibi</div>
          <div className="welcome-hero-sub">
            Local-first approval gate for multi-vendor coding agents.
          </div>
        </div>
        <div className="welcome-cockpit" role="status" aria-label="Cockpit summary">
          <span
            className={`welcome-pill ${pendingApprovals > 0 ? "attention" : ""}`}
            title={
              pendingApprovals > 0
                ? "Approvals appear inline in the active session"
                : "No pending approvals"
            }
          >
            <i
              className={`codicon ${pendingApprovals > 0 ? "codicon-bell-dot" : "codicon-bell"}`}
              aria-hidden="true"
            />
            <span>{pendingApprovals} pending</span>
          </span>
          <span className="welcome-pill" title="Active agent sessions">
            <i className="codicon codicon-rocket" aria-hidden="true" />
            <span>{activeSessions} session{activeSessions === 1 ? "" : "s"}</span>
          </span>
          <span
            className={`welcome-pill ${runningTransport ? "ok" : ""}`}
            title={runningTransport ? `Phone transport: ${runningTransport.label}` : "No transport — phone cannot pair"}
          >
            <i
              className={`codicon ${runningTransport ? "codicon-broadcast" : "codicon-circle-slash"}`}
              aria-hidden="true"
            />
            <span>{runningTransport ? runningTransport.label : "no transport"}</span>
          </span>
        </div>
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
                  onClick={() => {
                    setActiveWorkspace(workspace.id);
                    setRightDockView("files");
                  }}
                  title={workspace.path}
                >
                  <i className="codicon codicon-folder" aria-hidden="true" />
                  <span>{workspace.name}</span>
                  <span className="welcome-muted welcome-path">{workspace.path}</span>
                </button>
              ))
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
