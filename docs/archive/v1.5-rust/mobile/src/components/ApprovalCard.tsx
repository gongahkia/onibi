import { useRef, useState } from "react";
import type { PointerEvent } from "react";
import {
  approvalRiskBadges,
  approvalSupportsUpdatedInput,
  commandText,
  swipeDecision,
} from "../approvals";
import type { Approval, Decision } from "../types";
import { formatTime } from "../utils";

export function ApprovalCard({
  approval,
  targeted,
  readOnly,
  onDecide,
  onReply,
}: {
  approval: Approval;
  targeted?: boolean;
  readOnly?: boolean;
  onDecide: (
    approval: Approval,
    decision: Decision,
    editedCommand?: string,
    reason?: string,
  ) => Promise<void>;
  onReply: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [command, setCommand] = useState(commandText(approval.input));
  const [denyReason, setDenyReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [swipeX, setSwipeX] = useState(0);
  const swipeStart = useRef<number | null>(null);
  const risks = approvalRiskBadges(approval, command);
  const canEdit = !readOnly && approvalSupportsUpdatedInput(approval);
  const swipeDisabled = editing || busy || readOnly;

  const submit = async (decision: Decision, editedCommand?: string, reason?: string) => {
    if (readOnly) {
      return;
    }
    setBusy(true);
    try {
      await onDecide(approval, decision, editedCommand, reason);
    } finally {
      setBusy(false);
    }
  };

  const onPointerDown = (event: PointerEvent<HTMLElement>) => {
    if (swipeDisabled) {
      return;
    }
    swipeStart.current = event.clientX;
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const onPointerMove = (event: PointerEvent<HTMLElement>) => {
    if (swipeStart.current === null || swipeDisabled) {
      return;
    }
    setSwipeX(Math.max(-140, Math.min(140, event.clientX - swipeStart.current)));
  };

  const onPointerEnd = (event: PointerEvent<HTMLElement>) => {
    if (swipeStart.current === null) {
      return;
    }
    const delta = event.clientX - swipeStart.current;
    swipeStart.current = null;
    setSwipeX(0);
    const decision = swipeDecision(delta, event.currentTarget.clientWidth);
    if (decision === "allow") {
      void submit("allow");
    } else if (decision === "deny") {
      void submit("deny", undefined, denyReason.trim());
    }
  };

  return (
    <article
      className={`approval-card${targeted ? " targeted" : ""}${readOnly ? " read-only" : ""}`}
      style={{ transform: swipeX ? `translateX(${swipeX}px)` : undefined }}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={() => {
        swipeStart.current = null;
        setSwipeX(0);
      }}
    >
      <div className="approval-header">
        <div>
          <p className="eyebrow">{approval.agent}</p>
          <h2>{approval.tool}</h2>
        </div>
        <time>{formatTime(approval.created_at)}</time>
      </div>
      <p className="cwd">{approval.cwd}</p>
      {risks.length > 0 ? (
        <div className="risk-list" aria-label="Approval risk indicators">
          {risks.map((risk) => <span key={risk}>{risk}</span>)}
        </div>
      ) : null}
      {editing && canEdit ? (
        <div className="edit-panel">
          <textarea
            value={command}
            onChange={(event) => setCommand(event.target.value)}
            rows={5}
            aria-label="Edited command"
            autoFocus
          />
          <button type="button" className="ghost-button" onClick={() => {
            setCommand(commandText(approval.input));
            setEditing(false);
          }}>
            Cancel edit
          </button>
        </div>
      ) : (
        <pre>{command}</pre>
      )}
      {!editing && !readOnly ? (
        <textarea
          value={denyReason}
          onChange={(event) => setDenyReason(event.target.value)}
          rows={2}
          aria-label="Deny reason"
          placeholder="Deny reason (optional)"
        />
      ) : null}
      {readOnly ? <p className="readonly-note">Read-only spectator session. Decisions are disabled.</p> : null}
      {!readOnly ? <div className="approval-actions">
        <button type="button" className="ghost-button" disabled={busy} onClick={onReply}>
          Reply
        </button>
        <button type="button" disabled={busy} onClick={() => void submit("allow")}>
          Allow
        </button>
        {editing && canEdit ? (
          <button type="button" disabled={busy} onClick={() => void submit("allow", command)}>
            Allow edited
          </button>
        ) : canEdit ? (
          <button type="button" disabled={busy} onClick={() => setEditing(true)}>
            Edit
          </button>
        ) : null}
        <button
          type="button"
          className="danger-button"
          disabled={busy}
          onClick={() => void submit("deny", undefined, denyReason.trim())}
        >
          Deny
        </button>
      </div> : null}
    </article>
  );
}
