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
      <main className="screen">
        <header className="screen-header with-actions">
          <h1>Session Missing</h1>
          <button type="button" className="button-secondary" onClick={onBack}>
            Back
          </button>
        </header>
        <p className="empty-state">This session is no longer active.</p>
      </main>
    );
  }

  const inputDisabled = realtimeState !== "authenticated" || !session.isControllable;

  return (
    <main className="screen screen-live">
      <header className="screen-header with-actions">
        <div>
          <h1>{session.displayName}</h1>
          <p>
            {session.status} • Realtime: {realtimeState}
          </p>
        </div>
        <div className="header-actions">
          <button type="button" className="button-secondary" onClick={onReloadBuffer}>
            Reload Buffer
          </button>
          <button type="button" className="button-secondary" onClick={onBack}>
            Back
          </button>
        </div>
      </header>

      <SessionOutputPane
        entries={outputEntries}
        onTerminalInput={onTerminalInput}
        onTerminalResize={onTerminalResize}
      />

      {inputError && <p className="error-text">{inputError}</p>}

      <RemoteInputBar disabled={inputDisabled} onSubmitLine={onSendLine} />
      <RemoteKeyStrip disabled={inputDisabled} onSendKey={onSendKey} />
    </main>
  );
}
