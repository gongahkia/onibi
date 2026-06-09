import { useEffect, useMemo, useState } from "react";
import { confirmAction } from "../lib/native-dialogs";
import {
  listPaneTargets,
  RemotePaneApiError,
  sendPanePreset,
  sendPaneText,
  type RemotePaneTarget,
} from "../lib/remote-keystrokes";

interface RemoteKeystrokeDialogProps {
  activeSessionId: string | null;
  open: boolean;
  onClose: () => void;
}

interface RemotePreset {
  key: string;
  label: string;
  destructive: boolean;
}

const REMOTE_PRESETS: RemotePreset[] = [
  { key: "continue", label: "Continue", destructive: false },
  { key: "approve", label: "Approve", destructive: false },
  { key: "interrupt", label: "Interrupt", destructive: true },
];

export function RemoteKeystrokeDialog({
  activeSessionId,
  open,
  onClose,
}: RemoteKeystrokeDialogProps) {
  const [targets, setTargets] = useState<RemotePaneTarget[]>([]);
  const [targetId, setTargetId] = useState("");
  const [text, setText] = useState("");
  const [sendEnter, setSendEnter] = useState(true);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    setStatus(null);
    void listPaneTargets()
      .then((nextTargets) => {
        if (cancelled) {
          return;
        }
        setTargets(nextTargets);
        setTargetId((current) => {
          if (nextTargets.some((target) => target.paneId === current)) {
            return current;
          }
          const active = nextTargets.find(
            (target) =>
              target.paneId === activeSessionId || target.sessionId === activeSessionId,
          );
          return active?.paneId ?? nextTargets[0]?.paneId ?? "";
        });
      })
      .catch((caught) => {
        if (!cancelled) {
          setTargets([]);
          setTargetId("");
          setError(errorMessage(caught));
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [activeSessionId, open]);

  const selectedTarget = useMemo(
    () => targets.find((target) => target.paneId === targetId),
    [targetId, targets],
  );
  const disabled = loading || busy || !selectedTarget;

  if (!open) {
    return null;
  }

  async function dispatchText() {
    if (!selectedTarget) {
      setError("Choose a target pane.");
      return;
    }
    if (!text && !sendEnter) {
      setError("Text or Enter is required.");
      return;
    }
    await dispatchWithConfirmation(
      selectedTarget,
      false,
      "Send text",
      (confirmed) => sendPaneText(selectedTarget.paneId, text, sendEnter, confirmed),
      () => setText(""),
    );
  }

  async function dispatchPreset(preset: RemotePreset) {
    if (!selectedTarget) {
      setError("Choose a target pane.");
      return;
    }
    await dispatchWithConfirmation(
      selectedTarget,
      preset.destructive,
      preset.label,
      (confirmed) => sendPanePreset(selectedTarget.paneId, preset.key, confirmed),
    );
  }

  async function dispatchWithConfirmation(
    target: RemotePaneTarget,
    destructive: boolean,
    actionLabel: string,
    send: (confirmed: boolean) => Promise<{ bytes: number }>,
    onSent?: () => void,
  ) {
    setBusy(true);
    setError(null);
    setStatus(null);
    try {
      const confirmed = await confirmIfNeeded(target, destructive, actionLabel);
      if (!confirmed) {
        return;
      }
      const result = await send(needsConfirmation(target, destructive));
      onSent?.();
      setStatus(`Sent ${result.bytes} bytes to ${target.label}.`);
    } catch (caught) {
      if (caught instanceof RemotePaneApiError && caught.status === 409) {
        const confirmed = await confirmAction(
          confirmationMessage(target, destructive, actionLabel),
          { okLabel: "Send", title: "Confirm Remote Input" },
        );
        if (confirmed) {
          try {
            const result = await send(true);
            onSent?.();
            setStatus(`Sent ${result.bytes} bytes to ${target.label}.`);
            return;
          } catch (retryError) {
            setError(errorMessage(retryError));
            return;
          }
        }
        return;
      }
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="modal-backdrop remote-keystroke-dialog"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="remote-keystroke-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="remote-keystroke-title">
            Remote Pane Input
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close remote pane input"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="modal-body">
          <label className="field-label">
            Target
            <select
              className="settings-select"
              value={targetId}
              disabled={loading || busy || targets.length === 0}
              onChange={(event) => setTargetId(event.target.value)}
              aria-label="Target pane"
            >
              {targets.length === 0 ? <option value="">No target panes</option> : null}
              {targets.map((target) => (
                <option key={target.paneId} value={target.paneId}>
                  {target.label} - {target.status} - {target.trustMode}
                </option>
              ))}
            </select>
          </label>
          <label className="field-label">
            Text
            <textarea
              className="remote-keystroke-textarea"
              value={text}
              disabled={disabled}
              rows={6}
              aria-label="Text to send"
              onChange={(event) => setText(event.target.value)}
            />
          </label>
          <label className="settings-checkbox-row">
            <input
              type="checkbox"
              checked={sendEnter}
              disabled={disabled}
              onChange={(event) => setSendEnter(event.target.checked)}
            />
            <span>Send Enter</span>
          </label>
          <div className="remote-keystroke-presets" aria-label="Remote input presets">
            {REMOTE_PRESETS.map((preset) => (
              <button
                key={preset.key}
                type="button"
                className={preset.destructive ? "text-button danger" : "text-button"}
                disabled={disabled}
                onClick={() => void dispatchPreset(preset)}
              >
                {preset.label}
              </button>
            ))}
          </div>
          {loading ? <div className="settings-note">Loading target panes...</div> : null}
          {status ? <div className="settings-note">{status}</div> : null}
          {error ? <div className="editor-error">{error}</div> : null}
          <footer className="dialog-actions">
            <button type="button" className="text-button" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="text-button primary"
              disabled={disabled || (!text && !sendEnter)}
              onClick={() => void dispatchText()}
            >
              {busy ? "Sending" : "Send"}
            </button>
          </footer>
        </div>
      </section>
    </div>
  );
}

function needsConfirmation(target: RemotePaneTarget, destructive: boolean): boolean {
  return destructive || target.trustMode === "approval-required";
}

async function confirmIfNeeded(
  target: RemotePaneTarget,
  destructive: boolean,
  actionLabel: string,
): Promise<boolean> {
  if (!needsConfirmation(target, destructive)) {
    return true;
  }
  return confirmAction(confirmationMessage(target, destructive, actionLabel), {
    okLabel: "Send",
    title: "Confirm Remote Input",
  });
}

function confirmationMessage(
  target: RemotePaneTarget,
  destructive: boolean,
  actionLabel: string,
): string {
  if (destructive) {
    return `${actionLabel} on ${target.label}? This can interrupt the running process.`;
  }
  return `${actionLabel} to ${target.label}? This session requires approval confirmation.`;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
