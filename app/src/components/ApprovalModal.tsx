import { useEffect, useMemo, useState, type CSSProperties } from "react";
import {
  decideApproval,
  subscribeApprovalEvents,
  storedApprovalPort,
  type ApprovalPendingMessage,
  type ApprovalRealtimeMessage,
} from "../lib/approval-client";

export interface ApprovalModalProps {
  token?: string;
  port?: number;
  initialPending?: ApprovalPendingMessage | null;
  onResolved?: (approvalId: string) => void;
}

export function ApprovalModal({
  token,
  port,
  initialPending = null,
  onResolved,
}: ApprovalModalProps) {
  const effectivePort = port ?? storedApprovalPort() ?? 17893;
  const [pending, setPending] = useState<ApprovalPendingMessage | null>(initialPending);
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

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
          setPending(message);
          setEditing(false);
          setEditValue(formatInput(message));
          setError(null);
          setToast(null);
        }
        if (message.type === "approval-resolved") {
          setPending((current) => {
            if (current?.approval_id === message.approval_id) {
              setToast(`Resolved on ${message.by ?? "another client"}`);
              onResolved?.(message.approval_id);
              return null;
            }
            return current;
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
      setPending(initialPending);
      setEditValue(formatInput(initialPending));
    }
  }, [initialPending]);

  const formattedInput = useMemo(() => {
    if (!pending) {
      return "";
    }
    return formatInput(pending);
  }, [pending]);

  if (!pending) {
    return toast ? <div style={toastStyle}>{toast}</div> : null;
  }

  const submit = async (
    decision: "allow" | "deny",
    updatedInput?: unknown,
    reason?: string,
  ) => {
    try {
      await decideApproval({
        port: effectivePort,
        token,
        approvalId: pending.approval_id,
        decision,
        updatedInput,
        reason,
      });
      onResolved?.(pending.approval_id);
      setPending(null);
      setToast(null);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "approval request failed");
    }
  };

  return (
    <div style={backdropStyle} role="presentation">
      <section aria-label="Approval request" role="dialog" style={modalStyle}>
        <header style={headerStyle}>
          <div>
            <div style={agentStyle}>{pending.agent}</div>
            <div style={cwdStyle}>{pending.cwd || "No working directory"}</div>
          </div>
          <span style={toolStyle}>{pending.tool}</span>
        </header>

        <pre style={preStyle}>{formattedInput}</pre>

        {editing ? (
          <div style={editWrapStyle}>
            <textarea
              aria-label="Edited tool input"
              value={editValue}
              onChange={(event) => setEditValue(event.target.value)}
              style={textareaStyle}
            />
            <div style={actionsStyle}>
              <button style={buttonStyle} type="button" onClick={() => setEditing(false)}>
                Cancel
              </button>
              <button
                style={{ ...buttonStyle, ...editButtonStyle }}
                type="button"
                onClick={() => void submit("allow", updatedInputFor(pending, editValue))}
              >
                Submit Edit
              </button>
            </div>
          </div>
        ) : (
          <div style={actionsStyle}>
            <button
              style={{ ...buttonStyle, ...allowButtonStyle }}
              type="button"
              onClick={() => void submit("allow")}
            >
              Allow
            </button>
            <button
              style={{ ...buttonStyle, ...editButtonStyle }}
              type="button"
              onClick={() => {
                setEditing(true);
                setEditValue(formattedInput);
              }}
            >
              Edit
            </button>
            <button
              style={{ ...buttonStyle, ...denyButtonStyle }}
              type="button"
              onClick={() => void submit("deny", undefined, "denied by user")}
            >
              Deny
            </button>
          </div>
        )}

        {error ? <div style={errorStyle}>{error}</div> : null}
      </section>
    </div>
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
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

const backdropStyle: CSSProperties = {
  position: "fixed",
  inset: 0,
  display: "grid",
  placeItems: "center",
  background: "rgba(8, 10, 14, 0.56)",
  zIndex: 1000,
};

const modalStyle: CSSProperties = {
  width: "min(720px, calc(100vw - 32px))",
  maxHeight: "min(680px, calc(100vh - 32px))",
  overflow: "auto",
  borderRadius: 8,
  border: "1px solid #2d3748",
  background: "#111827",
  color: "#f9fafb",
  boxShadow: "0 24px 80px rgba(0, 0, 0, 0.4)",
  padding: 20,
};

const headerStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  gap: 16,
  justifyContent: "space-between",
  marginBottom: 16,
};

const agentStyle: CSSProperties = {
  fontSize: 18,
  fontWeight: 700,
};

const cwdStyle: CSSProperties = {
  color: "#9ca3af",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 12,
  marginTop: 4,
  overflowWrap: "anywhere",
};

const toolStyle: CSSProperties = {
  border: "1px solid #4b5563",
  borderRadius: 999,
  color: "#d1d5db",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 12,
  padding: "4px 10px",
};

const preStyle: CSSProperties = {
  background: "#030712",
  border: "1px solid #1f2937",
  borderRadius: 6,
  color: "#f3f4f6",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 13,
  lineHeight: 1.5,
  margin: 0,
  maxHeight: 300,
  overflow: "auto",
  padding: 12,
  whiteSpace: "pre-wrap",
  wordBreak: "break-word",
};

const editWrapStyle: CSSProperties = {
  display: "grid",
  gap: 12,
  marginTop: 14,
};

const textareaStyle: CSSProperties = {
  background: "#030712",
  border: "1px solid #4b5563",
  borderRadius: 6,
  color: "#f9fafb",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 13,
  minHeight: 160,
  padding: 12,
  resize: "vertical",
};

const actionsStyle: CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: 10,
  justifyContent: "flex-end",
  marginTop: 16,
};

const buttonStyle: CSSProperties = {
  border: 0,
  borderRadius: 6,
  color: "#111827",
  cursor: "pointer",
  fontWeight: 700,
  minWidth: 88,
  padding: "9px 14px",
};

const allowButtonStyle: CSSProperties = {
  background: "#34d399",
};

const editButtonStyle: CSSProperties = {
  background: "#fbbf24",
};

const denyButtonStyle: CSSProperties = {
  background: "#f87171",
};

const errorStyle: CSSProperties = {
  color: "#fecaca",
  fontSize: 13,
  marginTop: 12,
};

const toastStyle: CSSProperties = {
  bottom: 20,
  position: "fixed",
  right: 20,
  background: "#111827",
  border: "1px solid #374151",
  borderRadius: 6,
  color: "#f9fafb",
  padding: "10px 12px",
  zIndex: 1001,
};
