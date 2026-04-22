import { useEffect, useState } from "react";
import { RemoteInputBar } from "../components/RemoteInputBar";
import { RemoteKeyStrip } from "../components/RemoteKeyStrip";
import { SessionOutputPane } from "../components/SessionOutputPane";
import type { OutputEntry } from "../store/sessionStore";
import type { ControllableSessionSnapshot, RemoteInputKey } from "../types";

interface LiveSessionViewProps {
  session: ControllableSessionSnapshot | null;
  outputEntries: OutputEntry[];
  realtimeState: string;
  inputError: string | null;
  onBack: () => void;
  onReloadBuffer: () => void;
  onSendLine: (text: string) => void;
  onPasteText: (text: string) => void;
  onUploadFile: (file: File) => void;
  onSendKey: (key: RemoteInputKey) => void;
  onTerminalInput: (data: string) => void;
  onTerminalResize: (cols: number, rows: number) => void;
}

function realtimeBadgeClass(realtimeState: string): string {
  if (realtimeState === "authenticated") return "mf-badge mf-badge-compact mf-badge-ok";
  if (realtimeState === "reconnecting" || realtimeState === "connecting" || realtimeState === "authenticating") {
    return "mf-badge mf-badge-compact mf-badge-warn";
  }
  return "mf-badge mf-badge-compact";
}

function realtimeShortLabel(realtimeState: string): string {
  switch (realtimeState) {
    case "authenticated": return "Live";
    case "connecting": return "Connecting";
    case "authenticating": return "Auth";
    case "reconnecting": return "Reconnecting";
    default: return "Offline";
  }
}

function statusClassName(status: ControllableSessionSnapshot["status"]): string {
  switch (status) {
    case "running": return "status-pill status-running";
    case "starting": return "status-pill status-starting";
    case "exited": return "status-pill status-exited";
    case "failed": return "status-pill status-failed";
    default: return "status-pill";
  }
}

function shellName(shell?: string | null): string | null {
  if (!shell) return null;
  const parts = shell.split("/");
  return parts[parts.length - 1] || shell;
}

function pathName(path?: string | null): string | null {
  if (!path) return null;
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] || path;
}

function formatBytes(bytes?: number): string | null {
  if (bytes === undefined) return null;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function IconBack(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M15 18l-6-6 6-6" />
    </svg>
  );
}

function IconReload(): JSX.Element {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-3-6.7" />
      <path d="M21 4v5h-5" />
    </svg>
  );
}

function useIsLandscape(): boolean {
  const [isLandscape, setIsLandscape] = useState(() =>
    typeof window !== "undefined" ? window.matchMedia("(orientation: landscape) and (min-width: 720px)").matches : false
  );
  useEffect(() => {
    const mql = window.matchMedia("(orientation: landscape) and (min-width: 720px)");
    const handler = (e: MediaQueryListEvent) => setIsLandscape(e.matches);
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, []);
  return isLandscape;
}

export function LiveSessionView({
  session,
  outputEntries,
  realtimeState,
  inputError,
  onBack,
  onReloadBuffer,
  onSendLine,
  onPasteText,
  onUploadFile,
  onSendKey,
  onTerminalInput,
  onTerminalResize
}: LiveSessionViewProps): JSX.Element {
  const isLandscape = useIsLandscape();

  if (!session) {
    return (
      <main className="mf-page">
        <div className="mf-shell">
          <section className="mf-card mf-empty-state">
            <h1>Session Missing</h1>
            <p>This session is no longer available.</p>
            <button type="button" className="button-secondary" onClick={onBack}>
              Back
            </button>
          </section>
        </div>
      </main>
    );
  }

  const inputDisabled = realtimeState !== "authenticated" || !session.isControllable || session.status !== "running";
  const shell = shellName(session.shell);
  const cwd = pathName(session.workingDirectory);
  const terminalSize =
    session.terminalCols && session.terminalRows ? `${session.terminalCols}x${session.terminalRows}` : null;
  const lastBell = session.lastTerminalEvent?.kind === "bell";
  const flowControl = session.health?.flowControl;
  const outputBytes = formatBytes(session.health?.outputByteCount);

  return (
    <main className={`mf-live-page ${isLandscape ? "mf-live-landscape" : "mf-live-portrait"}`}>
      <header className="mf-live-header">
        <button type="button" className="mf-icon-btn" onClick={onBack} aria-label="Back" title="Back">
          <IconBack />
        </button>
        <div className="mf-live-title">
          <h1>{session.displayName}</h1>
          {session.terminalTitle && <p className="mf-live-subtitle">{session.terminalTitle}</p>}
          <div className="mf-live-badges">
            <span className={statusClassName(session.status)}>{session.status}</span>
            <span className={realtimeBadgeClass(realtimeState)} title={`Realtime: ${realtimeState}`}>
              <span className="mf-badge-dot" aria-hidden="true" />
              {realtimeShortLabel(realtimeState)}
            </span>
            {shell && <span className="mf-badge mf-badge-compact">{shell}</span>}
            {cwd && <span className="mf-badge mf-badge-compact" title={session.workingDirectory ?? undefined}>cwd {cwd}</span>}
            {terminalSize && <span className="mf-badge mf-badge-compact">{terminalSize}</span>}
            {flowControl && flowControl !== "open" && (
              <span className="mf-badge mf-badge-compact mf-badge-warn">flow {flowControl}</span>
            )}
            {outputBytes && <span className="mf-badge mf-badge-compact">out {outputBytes}</span>}
            {lastBell && <span className="mf-badge mf-badge-compact mf-badge-warn">bell</span>}
          </div>
        </div>
        <button
          type="button"
          className="mf-icon-btn"
          onClick={onReloadBuffer}
          aria-label="Reload buffer"
          title="Reload buffer"
        >
          <IconReload />
        </button>
      </header>

      {inputError && (
        <div className="mf-live-error-wrap">
          <p className="mf-alert mf-alert-error">{inputError}</p>
        </div>
      )}

      <section className="mf-live-terminal-wrap">
        <SessionOutputPane
          entries={outputEntries}
          onTerminalInput={onTerminalInput}
          onTerminalResize={onTerminalResize}
        />
      </section>

      {session.isControllable && (
        <>
          <section className="mf-live-keys">
            <RemoteKeyStrip
              disabled={inputDisabled}
              onSendKey={onSendKey}
              layout={isLandscape ? "column" : "row"}
            />
          </section>
          <section className="mf-live-input-footer">
            <RemoteInputBar
              disabled={inputDisabled}
              onSubmitLine={onSendLine}
              onPasteText={onPasteText}
              onUploadFile={onUploadFile}
              autoEnter
            />
          </section>
        </>
      )}
    </main>
  );
}
