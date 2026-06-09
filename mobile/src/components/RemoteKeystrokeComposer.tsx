import { useState } from "react";
import { REMOTE_PRESETS } from "../constants";
import {
  confirmationMessageForDispatch,
  needsRemoteConfirmation,
  sendRemotePresetRequest,
  sendRemoteTextRequest,
} from "../remote-pane";
import type {
  PaneSendResponse,
  PaneTarget,
  PendingRemoteDispatch,
} from "../types";

export function RemoteKeystrokeComposer({
  readOnly,
  targets,
  targetId,
  onTargetChange,
  fetchWithFallback,
  onSent,
}: {
  readOnly: boolean;
  targets: PaneTarget[];
  targetId: string;
  onTargetChange: (targetId: string) => void;
  fetchWithFallback: (path: string, init?: RequestInit) => Promise<Response>;
  onSent: () => void;
}) {
  const [text, setText] = useState("");
  const [sendEnter, setSendEnter] = useState(true);
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [pendingDispatch, setPendingDispatch] = useState<PendingRemoteDispatch | null>(null);
  const selectedTarget = targets.find((target) => target.paneId === targetId);
  const disabled = readOnly || busy || !selectedTarget;

  const dispatch = async (next: PendingRemoteDispatch, confirmed: boolean) => {
    if (readOnly) {
      setStatus("Read-only spectator session. Remote typing is disabled.");
      return;
    }
    if (!selectedTarget) {
      setStatus("Choose a target pane.");
      return;
    }
    if (!confirmed && needsRemoteConfirmation(selectedTarget, next.destructive)) {
      setPendingDispatch(next);
      setStatus(null);
      return;
    }
    setBusy(true);
    setStatus(null);
    try {
      const path =
        next.kind === "text"
          ? `/v1/panes/${encodeURIComponent(selectedTarget.paneId)}/send-text`
          : `/v1/panes/${encodeURIComponent(selectedTarget.paneId)}/send-keys`;
      const body =
        next.kind === "text"
          ? sendRemoteTextRequest(text, sendEnter, confirmed)
          : sendRemotePresetRequest(next.preset?.key ?? "", confirmed);
      const response = await fetchWithFallback(path, {
        method: "POST",
        body: JSON.stringify(body),
      });
      const result = (await response.json()) as PaneSendResponse;
      setPendingDispatch(null);
      setStatus(`Sent ${result.bytes} bytes to ${selectedTarget.label}.`);
      if (next.kind === "text") {
        setText("");
      }
      onSent();
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : String(caught);
      if (message.includes("HTTP 409")) {
        setPendingDispatch(next);
        setStatus(null);
      } else {
        setStatus(message);
      }
    } finally {
      setBusy(false);
    }
  };

  const submitText = () => {
    const hasPayload = text.length > 0 || sendEnter;
    if (!hasPayload) {
      setStatus("Text or Enter is required.");
      return;
    }
    void dispatch(
      {
        kind: "text",
        destructive: false,
        message: "Send text to this pane?",
      },
      false,
    );
  };

  return (
    <section className="remote-composer" aria-label="Send text to pane">
      <div className="composer-header">
        <div>
          <p className="eyebrow">Remote input</p>
          <h2>Send to pane</h2>
        </div>
        {readOnly ? <span className="scope-pill">Read only</span> : null}
      </div>
      <label>
        <span>Target</span>
        <select
          value={targetId}
          disabled={readOnly || busy || targets.length === 0}
          onChange={(event) => onTargetChange(event.target.value)}
          aria-label="Target pane"
        >
          {targets.length === 0 ? <option value="">No panes</option> : null}
          {targets.length > 0 ? <option value="">Choose target</option> : null}
          {targets.map((target) => (
            <option key={target.paneId} value={target.paneId}>
              {target.label} · {target.status} · {target.trustMode}
            </option>
          ))}
        </select>
      </label>
      <textarea
        value={text}
        disabled={disabled}
        onChange={(event) => setText(event.target.value)}
        rows={5}
        aria-label="Text to send"
        placeholder="Text to send"
      />
      <label className="toggle-row">
        <input
          type="checkbox"
          checked={sendEnter}
          disabled={disabled}
          onChange={(event) => setSendEnter(event.target.checked)}
        />
        <span>Send Enter</span>
      </label>
      <div className="preset-row" aria-label="Remote input presets">
        {REMOTE_PRESETS.map((preset) => (
          <button
            key={preset.key}
            type="button"
            className={preset.destructive ? "danger-button" : "ghost-button"}
            disabled={disabled}
            onClick={() =>
              void dispatch(
                {
                  kind: "preset",
                  preset,
                  destructive: preset.destructive,
                  message: `${preset.label} on this pane?`,
                },
                false,
              )
            }
          >
            {preset.label}
          </button>
        ))}
      </div>
      <div className="approval-actions">
        <button type="button" disabled={disabled} onClick={submitText}>
          Send text
        </button>
      </div>
      {pendingDispatch ? (
        <div className="confirm-sheet" role="alert">
          <strong>{confirmationMessageForDispatch(selectedTarget, pendingDispatch)}</strong>
          <div className="approval-actions">
            <button
              type="button"
              className="ghost-button"
              disabled={busy}
              onClick={() => setPendingDispatch(null)}
            >
              Cancel
            </button>
            <button
              type="button"
              className={pendingDispatch.destructive ? "danger-button" : ""}
              disabled={busy}
              onClick={() => void dispatch(pendingDispatch, true)}
            >
              Confirm
            </button>
          </div>
        </div>
      ) : null}
      {readOnly ? (
        <p className="readonly-note">Read-only spectator session. Remote typing is disabled.</p>
      ) : null}
      {status ? <p className={status.includes("failed") ? "error-line" : "status-line"}>{status}</p> : null}
    </section>
  );
}
