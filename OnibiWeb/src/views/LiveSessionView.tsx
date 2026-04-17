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
  onSendKey: (key: RemoteInputKey) => void;
  onTerminalInput: (data: string) => void;
  onTerminalResize: (cols: number, rows: number) => void;
}

function realtimeBadgeClass(realtimeState: string): string {
  if (realtimeState === "authenticated") {
    return "mf-badge mf-badge-ok";
  }
  if (realtimeState === "reconnecting" || realtimeState === "connecting" || realtimeState === "authenticating") {
    return "mf-badge mf-badge-warn";
  }
  return "mf-badge";
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

export function LiveSessionView({
  session,
  outputEntries,
  realtimeState,
  inputError,
  onBack,
  onReloadBuffer,
  onSendLine,
  onSendKey,
  onTerminalInput,
  onTerminalResize
}: LiveSessionViewProps): JSX.Element {
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

  return (
    <main className="mf-live-page">
      <header className="mf-live-header">
        <div className="mf-live-header-main">
          <button type="button" className="button-secondary" onClick={onBack}>
            Back
          </button>
          <div className="mf-live-title">
            <h1>{session.displayName}</h1>
            <div className="mf-live-badges">
              <span className={statusClassName(session.status)}>{session.status}</span>
              <span className={realtimeBadgeClass(realtimeState)}>Realtime: {realtimeState}</span>
            </div>
          </div>
        </div>
        <button type="button" className="button-secondary" onClick={onReloadBuffer}>
          Reload Buffer
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
        <section className="mf-live-controls">
          <p className="mf-live-hint">Tap terminal output to focus and type directly. Send appends Enter by default.</p>
          <RemoteKeyStrip disabled={inputDisabled} onSendKey={onSendKey} />
          <RemoteInputBar disabled={inputDisabled} onSubmitLine={onSendLine} autoEnter />
        </section>
      )}
    </main>
  );
}
