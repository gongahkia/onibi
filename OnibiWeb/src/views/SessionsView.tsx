import { useEffect, useRef, useState } from "react";
import type { ControllableSessionSnapshot, DiagnosticsResponse } from "../types";

interface SessionsViewProps {
  sessions: ControllableSessionSnapshot[];
  outputPreviewBySession: Record<string, string>;
  realtimeState: string;
  diagnostics: DiagnosticsResponse | null;
  onOpenSession: (sessionId: string) => void;
  onRefresh: () => void;
  onDisconnect: () => void;
  onRemoveSession: (sessionId: string) => void;
  onRenameSession: (sessionId: string, nextName: string) => void;
}

function statusClassName(status: ControllableSessionSnapshot["status"]): string {
  switch (status) {
    case "running":
      return "status-pill status-running";
    case "starting":
      return "status-pill status-starting";
    case "exited":
      return "status-pill status-exited";
    case "failed":
      return "status-pill status-failed";
    default:
      return "status-pill";
  }
}

function realtimeBadgeClass(realtimeState: string): string {
  if (realtimeState === "authenticated") {
    return "mf-badge mf-badge-compact mf-badge-ok";
  }
  if (realtimeState === "reconnecting" || realtimeState === "connecting" || realtimeState === "authenticating") {
    return "mf-badge mf-badge-compact mf-badge-warn";
  }
  return "mf-badge mf-badge-compact";
}

function realtimeShortLabel(realtimeState: string): string {
  switch (realtimeState) {
    case "authenticated":
      return "Live";
    case "connecting":
      return "Connecting";
    case "authenticating":
      return "Auth";
    case "reconnecting":
      return "Reconnecting";
    default:
      return "Offline";
  }
}

function formatActivity(isoTimestamp: string): string {
  const timestamp = Date.parse(isoTimestamp);
  if (!Number.isFinite(timestamp)) return "unknown";
  const deltaMs = Date.now() - timestamp;
  if (deltaMs < 60_000) return "just now";
  if (deltaMs < 3_600_000) return `${Math.floor(deltaMs / 60_000)}m ago`;
  if (deltaMs < 86_400_000) return `${Math.floor(deltaMs / 3_600_000)}h ago`;
  return `${Math.floor(deltaMs / 86_400_000)}d ago`;
}

function shellName(shell?: string | null): string | null {
  if (!shell) return null;
  const parts = shell.split("/");
  return parts[parts.length - 1] || shell;
}

function metadataLabels(session: ControllableSessionSnapshot): string[] {
  const labels: string[] = [];
  const shell = shellName(session.shell);
  if (shell) labels.push(shell);
  if (session.pid) labels.push(`pid ${session.pid}`);
  if (session.terminalCols && session.terminalRows) {
    labels.push(`${session.terminalCols}x${session.terminalRows}`);
  }
  if (session.hostname) labels.push(session.hostname);
  return labels;
}

function IconRefresh(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-3-6.7" />
      <path d="M21 4v5h-5" />
    </svg>
  );
}

function IconDisconnect(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M16 17l5-5-5-5" />
      <path d="M21 12H9" />
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
    </svg>
  );
}

function IconDots(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="5" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="12" cy="19" r="1" />
    </svg>
  );
}

function IconTrash(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 6h18" />
      <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
      <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
    </svg>
  );
}

function IconPencil(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" />
    </svg>
  );
}

export function SessionsView({
  sessions,
  outputPreviewBySession,
  realtimeState,
  diagnostics,
  onOpenSession,
  onRefresh,
  onDisconnect,
  onRemoveSession,
  onRenameSession
}: SessionsViewProps): JSX.Element {
  const [openMenuId, setOpenMenuId] = useState<string | null>(null);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const menuRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!openMenuId) return;
    const close = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setOpenMenuId(null);
    };
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [openMenuId]);

  return (
    <main className="mf-page">
      <div className="mf-shell">
        <header className="mf-card mf-header-card">
          <div className="mf-header-top">
            <div className="mf-header-titles">
              <h1>Terminal Sessions</h1>
              <p>Choose a session to open live control.</p>
            </div>
            <span className={realtimeBadgeClass(realtimeState)} title={`Realtime: ${realtimeState}`}>
              <span className="mf-badge-dot" aria-hidden="true" />
              {realtimeShortLabel(realtimeState)}
            </span>
          </div>

          {realtimeState !== "authenticated" && sessions.length > 0 && (
            <p className="mf-alert mf-alert-info">Using cached sessions while realtime reconnects.</p>
          )}

          {diagnostics && (
            <div className="mf-runtime-strip" aria-label="Host runtime diagnostics">
              <span className="mf-runtime-chip">Host v{diagnostics.hostVersion}</span>
              {diagnostics.latestProxyVersion && (
                <span className="mf-runtime-chip">Proxy v{diagnostics.latestProxyVersion}</span>
              )}
              {diagnostics.realtimeProtocolVersion !== undefined && (
                <span className="mf-runtime-chip">
                  Realtime v{diagnostics.realtimeProtocolVersion}
                </span>
              )}
              {(diagnostics.proxyVersionMismatchCount ?? 0) > 0 && (
                <span className="mf-runtime-chip mf-runtime-chip-warn">
                  Proxy mismatches {diagnostics.proxyVersionMismatchCount}
                </span>
              )}
              {(diagnostics.websocketAuthFailureCount ?? 0) > 0 && (
                <span className="mf-runtime-chip mf-runtime-chip-warn">
                  WS auth fails {diagnostics.websocketAuthFailureCount}
                </span>
              )}
            </div>
          )}

          <div className="mf-header-actions mf-header-actions-icons">
            <button
              type="button"
              className="mf-icon-btn"
              onClick={onRefresh}
              aria-label="Refresh sessions"
              title="Refresh"
            >
              <IconRefresh />
            </button>
            <button
              type="button"
              className="mf-icon-btn mf-icon-btn-danger"
              onClick={onDisconnect}
              aria-label="Disconnect"
              title="Disconnect"
            >
              <IconDisconnect />
            </button>
          </div>
        </header>

        {sessions.length === 0 ? (
          <section className="mf-card mf-empty-state">
            <h2>No sessions available</h2>
            <p>No controllable terminal sessions are currently running on the host.</p>
          </section>
        ) : (
          <ul className="mf-session-list">
            {sessions.map((session) => {
              const outputPreview = outputPreviewBySession[session.id];
              const menuOpen = openMenuId === session.id;
              const metadata = metadataLabels(session);
              return (
                <li key={session.id} className="mf-session-li">
                  <button
                    type="button"
                    className="mf-session-card"
                    onClick={() => onOpenSession(session.id)}
                    disabled={!session.isControllable}
                  >
                    <div className="mf-session-card-top">
                      <strong>{session.displayName}</strong>
                      <span className={statusClassName(session.status)}>{session.status}</span>
                    </div>

                    {metadata.length > 0 && (
                      <div className="mf-session-meta-row" aria-label="Session metadata">
                        {metadata.map((label) => (
                          <span key={label}>{label}</span>
                        ))}
                      </div>
                    )}

                    {session.terminalTitle && (
                      <p className="mf-terminal-title" title={session.terminalTitle}>
                        {session.terminalTitle}
                      </p>
                    )}

                    {session.lastCommandPreview && (
                      <p className="mf-command-preview">{session.lastCommandPreview}</p>
                    )}

                    {outputPreview && (
                      <p className="mf-output-preview">{outputPreview}</p>
                    )}

                    <div className="mf-session-card-bottom">
                      <span>Last activity {formatActivity(session.lastActivityAt)}</span>
                      {!session.isControllable && <span className="mf-readonly-pill">Read-only</span>}
                    </div>
                  </button>

                  <div className="mf-session-menu-wrap" ref={menuOpen ? menuRef : undefined}>
                    <button
                      type="button"
                      className="mf-session-menu-btn"
                      aria-label="Session actions"
                      aria-expanded={menuOpen}
                      onClick={(e) => {
                        e.stopPropagation();
                        setOpenMenuId(menuOpen ? null : session.id);
                      }}
                    >
                      <IconDots />
                    </button>
                    {menuOpen && (
                      <div className="mf-session-menu" role="menu">
                        <button
                          type="button"
                          role="menuitem"
                          className="mf-session-menu-item"
                          onClick={(e) => {
                            e.stopPropagation();
                            setOpenMenuId(null);
                            setRenamingId(session.id);
                            setRenameDraft(session.displayName);
                          }}
                        >
                          <IconPencil />
                          <span>Rename</span>
                        </button>
                        <button
                          type="button"
                          role="menuitem"
                          className="mf-session-menu-item mf-session-menu-item-danger"
                          onClick={(e) => {
                            e.stopPropagation();
                            setOpenMenuId(null);
                            onRemoveSession(session.id);
                          }}
                        >
                          <IconTrash />
                          <span>Remove</span>
                        </button>
                      </div>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {renamingId && (
        <div
          className="mf-help-overlay"
          role="dialog"
          aria-modal="true"
          aria-label="Rename session"
          onClick={() => setRenamingId(null)}
        >
          <div className="mf-help-panel mf-rename-panel" onClick={(e) => e.stopPropagation()}>
            <header className="mf-help-header">
              <h2>Rename session</h2>
              <button
                type="button"
                className="mf-help-close"
                onClick={() => setRenamingId(null)}
                aria-label="Cancel rename"
              >
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M18 6L6 18M6 6l12 12" />
                </svg>
              </button>
            </header>
            <form
              className="mf-rename-form"
              onSubmit={(e) => {
                e.preventDefault();
                if (renameDraft.trim()) {
                  onRenameSession(renamingId, renameDraft);
                  setRenamingId(null);
                }
              }}
            >
              <input
                type="text"
                value={renameDraft}
                onChange={(e) => setRenameDraft(e.target.value)}
                placeholder="Session name"
                autoFocus
                spellCheck={false}
              />
              <div className="mf-rename-actions">
                <button type="button" className="button-secondary" onClick={() => setRenamingId(null)}>
                  Cancel
                </button>
                <button type="submit" className="mf-submit" disabled={!renameDraft.trim()}>
                  Save
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  );
}
