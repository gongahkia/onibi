import { useEffect, useRef, useState } from "react";
import {
  decideApproval,
  storedApprovalPort,
  subscribeApprovalEvents,
  type ApprovalPendingMessage,
  type ApprovalRealtimeMessage,
} from "../lib/approval-client";
import { requestInformationalAttention } from "../lib/window-attention";

export interface ApprovalInlineBannerProps {
  token?: string;
  port?: number;
  initialPending?: ApprovalPendingMessage | null;
  onResolved?: (approvalId: string) => void;
}

export function ApprovalInlineBanner({
  token,
  port,
  initialPending = null,
  onResolved,
}: ApprovalInlineBannerProps) {
  const effectivePort = port ?? storedApprovalPort() ?? 17893;
  const [queue, setQueue] = useState<ApprovalPendingMessage[]>(
    () => (initialPending ? [initialPending] : []),
  );
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState("");
  const [denyReason, setDenyReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const attentionSeen = useRef<Set<string>>(new Set());

  const currentPending = queue[0] ?? null;

  useEffect(() => {
    let disposed = false;
    let cleanup: (() => void) | undefined;
    void subscribeApprovalEvents(
      { token, port: effectivePort },
      (message: ApprovalRealtimeMessage) => {
        if (disposed) {
          return;
        }
        if (message.type === "approval-pending") {
          if (!attentionSeen.current.has(message.approval_id)) {
            attentionSeen.current.add(message.approval_id);
            window.dispatchEvent(
              new CustomEvent("onibi:approval-attention", {
                detail: {
                  approvalId: message.approval_id,
                  sessionId: message.session_id,
                  escalate: true,
                },
              }),
            );
            void requestInformationalAttention();
          }
          setQueue((items) => {
            const exists = items.some((item) => item.approval_id === message.approval_id);
            const next = exists
              ? items.map((item) =>
                  item.approval_id === message.approval_id ? message : item,
                )
              : [...items, message];
            if (items.length === 0) {
              setEditing(false);
              setEditValue(formatInput(message));
              setDenyReason("");
            }
            return next;
          });
          setError(null);
          setToast(null);
        }
        if (message.type === "approval-resolved") {
          setQueue((items) => {
            const resolvedFirst = items[0]?.approval_id === message.approval_id;
            const next = items.filter((item) => item.approval_id !== message.approval_id);
            if (resolvedFirst) {
              setToast(`Resolved on ${message.by ?? "another client"}`);
              onResolved?.(message.approval_id);
              setEditing(false);
              setEditValue(next[0] ? formatInput(next[0]) : "");
              setDenyReason("");
            }
            return next;
          });
        }
      },
    ).then((dispose) => {
      if (disposed) {
        dispose();
      } else {
        cleanup = dispose;
      }
    });
    return () => {
      disposed = true;
      cleanup?.();
    };
  }, [effectivePort, onResolved, token]);

  useEffect(() => {
    if (initialPending) {
      setQueue([initialPending]);
      setEditValue(formatInput(initialPending));
      setDenyReason("");
    }
  }, [initialPending]);

  useEffect(() => {
    if (currentPending && !editing) {
      setEditValue(formatInput(currentPending));
    }
  }, [currentPending, editing]);

  if (!currentPending) {
    return toast ? (
      <div className="approval-banner approval-banner-toast" role="status">
        {toast}
      </div>
    ) : null;
  }

  const formattedInput = formatInput(currentPending);
  const canEditInput = approvalSupportsUpdatedInput(currentPending);

  const submit = async (
    decision: "allow" | "deny",
    updatedInput?: unknown,
    reason?: string,
  ) => {
    try {
      await decideApproval({
        port: effectivePort,
        token,
        approvalId: currentPending.approval_id,
        decision,
        updatedInput,
        reason,
      });
      onResolved?.(currentPending.approval_id);
      setQueue((items) =>
        items.filter((item) => item.approval_id !== currentPending.approval_id),
      );
      setEditing(false);
      setEditValue("");
      setDenyReason("");
      setError(null);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "approval request failed");
    }
  };

  return (
    <section
      className="approval-banner"
      role="region"
      aria-label="Approval request"
      data-tool={currentPending.tool}
    >
      <header className="approval-banner-header">
        <span className="approval-banner-agent">{currentPending.agent}</span>
        <span className="approval-banner-tool">{currentPending.tool}</span>
        {currentPending.cwd ? (
          <span className="approval-banner-cwd" title={currentPending.cwd}>
            {currentPending.cwd}
          </span>
        ) : null}
        {queue.length > 1 ? (
          <span className="approval-banner-queue">
            1 of {queue.length}
          </span>
        ) : null}
      </header>
      {editing && canEditInput ? (
        <div className="approval-banner-edit">
          <textarea
            aria-label="Edited tool input"
            className="approval-banner-textarea"
            value={editValue}
            onChange={(event) => setEditValue(event.target.value)}
            rows={4}
          />
          <div className="approval-banner-actions">
            <button
              type="button"
              className="text-button"
              onClick={() => setEditing(false)}
            >
              Cancel
            </button>
            <button
              type="button"
              className="text-button primary"
              onClick={() => void submit("allow", updatedInputFor(currentPending, editValue))}
            >
              Edit & Allow
            </button>
          </div>
        </div>
      ) : (
        <>
          <pre className="approval-banner-preview" aria-label="Tool input preview">
            {formattedInput}
          </pre>
          <label className="approval-banner-deny">
            <span>Deny reason</span>
            <input
              type="text"
              value={denyReason}
              placeholder="optional"
              onChange={(event) => setDenyReason(event.target.value)}
            />
          </label>
          <div className="approval-banner-actions">
            <button
              type="button"
              className="text-button approval-allow"
              onClick={() => void submit("allow")}
            >
              Allow
            </button>
            {canEditInput ? (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setEditing(true);
                  setEditValue(formattedInput);
                }}
              >
                Edit input
              </button>
            ) : null}
            <button
              type="button"
              className="text-button approval-deny"
              onClick={() =>
                void submit("deny", undefined, denyReason.trim() || "denied by user")
              }
            >
              Deny
            </button>
          </div>
        </>
      )}
      {error ? <div className="approval-banner-error">{error}</div> : null}
    </section>
  );
}

function formatInput(message: ApprovalPendingMessage): string {
  if (
    message.tool === "Bash" &&
    typeof message.input === "object" &&
    message.input !== null &&
    "command" in message.input &&
    typeof (message.input as { command?: unknown }).command === "string"
  ) {
    return (message.input as { command: string }).command;
  }
  if (typeof message.input === "string") {
    return message.input;
  }
  return JSON.stringify(message.input, null, 2);
}

function updatedInputFor(message: ApprovalPendingMessage, value: string): unknown {
  if (
    message.tool === "Bash" &&
    typeof message.input === "object" &&
    message.input !== null &&
    "command" in message.input
  ) {
    return { ...(message.input as Record<string, unknown>), command: value };
  }
  if (typeof message.input === "string") {
    return value;
  }
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

function approvalSupportsUpdatedInput(message: ApprovalPendingMessage): boolean {
  if (!message.metadata || typeof message.metadata !== "object") {
    return true;
  }
  const metadata = message.metadata as Record<string, unknown>;
  return metadata.supportsUpdatedInput !== false;
}
