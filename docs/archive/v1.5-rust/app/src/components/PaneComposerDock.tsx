import { useEffect, useMemo, useState } from "react";
import { confirmAction } from "../lib/native-dialogs";
import {
  listPaneTargets,
  RemotePaneApiError,
  sendPanePreset,
  sendPaneText,
  type RemotePaneTarget,
} from "../lib/remote-keystrokes";

interface PaneComposerDockProps {
  activeSessionId: string | null;
}

interface Preset {
  key: string;
  label: string;
  destructive: boolean;
}

const PRESETS: Preset[] = [
  { key: "continue", label: "Continue", destructive: false },
  { key: "approve", label: "Approve", destructive: false },
  { key: "interrupt", label: "Interrupt", destructive: true },
];

export function PaneComposerDock({ activeSessionId }: PaneComposerDockProps) {
  const [collapsed, setCollapsed] = useState(true);
  useEffect(() => {
    function handleOpen() {
      setCollapsed(false);
    }
    window.addEventListener("onibi:open-pane-composer", handleOpen);
    return () => window.removeEventListener("onibi:open-pane-composer", handleOpen);
  }, []);
  const [targets, setTargets] = useState<RemotePaneTarget[]>([]);
  const [targetId, setTargetId] = useState("");
  const [text, setText] = useState("");
  const [sendEnter, setSendEnter] = useState(true);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (collapsed) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    setStatus(null);
    void listPaneTargets()
      .then((next) => {
        if (cancelled) {
          return;
        }
        setTargets(next);
        setTargetId((current) => {
          if (next.some((target) => target.paneId === current)) {
            return current;
          }
          const matched = next.find(
            (target) =>
              target.paneId === activeSessionId ||
              target.sessionId === activeSessionId,
          );
          return matched?.paneId ?? next[0]?.paneId ?? "";
        });
      })
      .catch((caught) => {
        if (cancelled) {
          return;
        }
        setTargets([]);
        setTargetId("");
        setError(errorMessage(caught));
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [activeSessionId, collapsed]);

  const selectedTarget = useMemo(
    () => targets.find((target) => target.paneId === targetId),
    [targetId, targets],
  );
  const disabled = loading || busy || !selectedTarget;

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

  async function dispatchPreset(preset: Preset) {
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

  if (collapsed) {
    return (
      <div className="pane-composer-dock collapsed">
        <button
          type="button"
          className="text-button"
          aria-label="Open remote pane composer"
          onClick={() => setCollapsed(false)}
        >
          <i className="codicon codicon-send" aria-hidden="true" />
          Remote input
        </button>
      </div>
    );
  }

  return (
    <section
      className="pane-composer-dock expanded"
      aria-label="Remote pane composer"
    >
      <header className="pane-composer-header">
        <select
          className="settings-select pane-composer-target"
          value={targetId}
          disabled={loading || busy || targets.length === 0}
          onChange={(event) => setTargetId(event.target.value)}
          aria-label="Target pane"
        >
          {targets.length === 0 ? <option value="">No target panes</option> : null}
          {targets.map((target) => (
            <option key={target.paneId} value={target.paneId}>
              {target.label} · {target.status} · {target.trustMode}
            </option>
          ))}
        </select>
        <div className="pane-composer-presets" aria-label="Remote input presets">
          {PRESETS.map((preset) => (
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
        <button
          type="button"
          className="icon-button"
          aria-label="Collapse remote pane composer"
          onClick={() => setCollapsed(true)}
        >
          <i className="codicon codicon-chevron-down" aria-hidden="true" />
        </button>
      </header>
      <div className="pane-composer-body">
        <textarea
          className="pane-composer-textarea"
          value={text}
          disabled={disabled}
          rows={3}
          aria-label="Text to send"
          placeholder="Type, then press Send or Enter…"
          onChange={(event) => setText(event.target.value)}
        />
        <div className="pane-composer-actions">
          <label className="settings-checkbox-row pane-composer-checkbox">
            <input
              type="checkbox"
              checked={sendEnter}
              disabled={disabled}
              onChange={(event) => setSendEnter(event.target.checked)}
            />
            <span>Send Enter</span>
          </label>
          <button
            type="button"
            className="text-button primary"
            disabled={disabled || (!text && !sendEnter)}
            onClick={() => void dispatchText()}
          >
            {busy ? "Sending" : "Send"}
          </button>
        </div>
      </div>
      {loading ? <div className="pane-composer-status">Loading target panes…</div> : null}
      {status ? <div className="pane-composer-status">{status}</div> : null}
      {error ? <div className="pane-composer-error">{error}</div> : null}
    </section>
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
