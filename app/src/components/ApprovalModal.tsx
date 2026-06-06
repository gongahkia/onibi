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
  const [pendingQueue, setPendingQueue] = useState<ApprovalPendingMessage[]>(
    () => (initialPending ? [initialPending] : []),
  );
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState("");
  const [denyReason, setDenyReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [now, setNow] = useState(Date.now());

  const pending = pendingQueue[0] ?? null;

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
          setPendingQueue((items) => {
            const exists = items.some((item) => item.approval_id === message.approval_id);
            if (items.length === 0) {
              setEditing(false);
              setEditValue(formatInput(message));
              setDenyReason("");
            }
            const next = exists
              ? items.map((item) => item.approval_id === message.approval_id ? message : item)
              : [...items, message];
            return sortApprovals(next);
          });
          setError(null);
          setToast(null);
        }
        if (message.type === "approval-resolved") {
          setPendingQueue((items) => {
            const resolvedCurrent = items[0]?.approval_id === message.approval_id;
            const next = items.filter((item) => item.approval_id !== message.approval_id);
            if (resolvedCurrent) {
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
      setPendingQueue([initialPending]);
      setEditValue(formatInput(initialPending));
      setDenyReason("");
    }
  }, [initialPending]);

  useEffect(() => {
    if (pending && !editing) {
      setEditValue(formatInput(pending));
    }
  }, [editing, pending]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const formattedInput = useMemo(() => {
    if (!pending) {
      return "";
    }
    return formatInput(pending);
  }, [pending]);

  const riskBadges = useMemo(
    () => pending ? approvalRiskBadges(pending, formattedInput) : [],
    [formattedInput, pending],
  );
  const policy = useMemo(
    () => pending ? approvalPolicyMetadata(pending.metadata) : null,
    [pending],
  );
  const timeRemaining = pending?.expires_at
    ? Math.max(0, pending.expires_at - now)
    : null;
  const showCountdown = typeof timeRemaining === "number" && timeRemaining <= 60_000;

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
      setPendingQueue((items) => {
        const next = items.filter((item) => item.approval_id !== pending.approval_id);
        setEditValue(next[0] ? formatInput(next[0]) : "");
        setDenyReason("");
        return next;
      });
      setEditing(false);
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
          <div style={headerMetaStyle}>
            {pendingQueue.length > 1 ? (
              <span style={queueStyle}>1 of {pendingQueue.length}</span>
            ) : null}
            {showCountdown ? (
              <span style={countdownStyle}>
                denies in {formatCountdown(timeRemaining ?? 0)}
              </span>
            ) : null}
            <span style={toolStyle}>{pending.tool}</span>
          </div>
        </header>

        {riskBadges.length > 0 ? (
          <div style={riskWrapStyle} aria-label="Approval risk indicators">
            {riskBadges.map((badge) => (
              <span key={badge} style={riskBadgeStyle}>{badge}</span>
            ))}
          </div>
        ) : null}
        {policy ? (
          <div style={policyStyle} aria-label="Matched approval policy">
            Policy {policy.name ? `"${policy.name}"` : "matched"} · {policy.decision}
          </div>
        ) : null}

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
                Approve edited command
              </button>
            </div>
          </div>
        ) : (
          <>
            <ApprovalPayloadPreview message={pending} fallback={formattedInput} />
            <label style={denyReasonLabelStyle}>
              Deny reason (optional)
              <input
                aria-label="Deny reason"
                value={denyReason}
                onChange={(event) => setDenyReason(event.target.value)}
                placeholder="Why this should not run"
                style={denyReasonInputStyle}
              />
            </label>
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
                Edit input
              </button>
              <button
                style={{ ...buttonStyle, ...denyButtonStyle }}
                type="button"
                onClick={() => void submit(
                  "deny",
                  undefined,
                  denyReason.trim() || "denied by user",
                )}
              >
                Deny
              </button>
            </div>
          </>
        )}

        {error ? <div style={errorStyle}>{error}</div> : null}
      </section>
    </div>
  );
}

function ApprovalPayloadPreview({
  fallback,
  message,
}: {
  fallback: string;
  message: ApprovalPendingMessage;
}) {
  const filePreview = fileEditPreview(message);
  if (!filePreview) {
    return <pre style={preStyle}>{fallback}</pre>;
  }
  return (
    <div style={filePreviewStyle} aria-label={`${message.tool} file change preview`}>
      <div style={filePreviewHeaderStyle}>
        <span>{filePreview.operation}</span>
        {filePreview.path ? <code>{filePreview.path}</code> : null}
      </div>
      {filePreview.sections.map((section, index) => (
        <div style={filePreviewSectionStyle} key={`${section.label}-${index}`}>
          <div style={filePreviewLabelStyle}>{section.label}</div>
          <pre style={filePreviewDiffStyle}>
            {section.rows.map((row, rowIndex) => (
              <span
                key={`${row.kind}-${rowIndex}`}
                style={{
                  ...filePreviewLineStyle,
                  ...(row.kind === "added" ? filePreviewAddedStyle : null),
                  ...(row.kind === "removed" ? filePreviewRemovedStyle : null),
                }}
              >
                {row.kind === "added" ? "+ " : row.kind === "removed" ? "- " : "  "}
                {row.text || " "}
              </span>
            ))}
          </pre>
        </div>
      ))}
    </div>
  );
}

function sortApprovals(items: ApprovalPendingMessage[]): ApprovalPendingMessage[] {
  return [...items].sort((left, right) => {
    const leftTime = typeof left.created_at === "number" ? left.created_at : Number.MAX_SAFE_INTEGER;
    const rightTime = typeof right.created_at === "number" ? right.created_at : Number.MAX_SAFE_INTEGER;
    return leftTime - rightTime;
  });
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

function fileEditPreview(message: ApprovalPendingMessage):
  | {
      operation: string;
      path?: string;
      sections: Array<{
        label: string;
        rows: Array<{ kind: "added" | "context" | "removed"; text: string }>;
      }>;
    }
  | null {
  if (!message.input || typeof message.input !== "object" || Array.isArray(message.input)) {
    return null;
  }
  const input = message.input as Record<string, unknown>;
  const tool = message.tool.toLowerCase();
  const path = stringProp(input, "file_path", "path", "filePath");
  if (tool === "write") {
    const content = stringProp(input, "content", "text", "new_string", "newString");
    if (content === undefined) {
      return null;
    }
    return {
      operation: "Write file",
      path,
      sections: [
        {
          label: "New content",
          rows: content.split("\n").map((line) => ({ kind: "added", text: line })),
        },
      ],
    };
  }
  if (tool === "edit") {
    const before = stringProp(input, "old_string", "oldString", "old");
    const after = stringProp(input, "new_string", "newString", "new");
    if (before === undefined || after === undefined) {
      return null;
    }
    return {
      operation: "Edit file",
      path,
      sections: [{ label: "Replacement", rows: lineDiff(before, after) }],
    };
  }
  if (tool === "multiedit") {
    const edits = Array.isArray(input.edits) ? input.edits : [];
    const sections = edits.flatMap((edit, index) => {
      if (!edit || typeof edit !== "object" || Array.isArray(edit)) {
        return [];
      }
      const record = edit as Record<string, unknown>;
      const before = stringProp(record, "old_string", "oldString", "old");
      const after = stringProp(record, "new_string", "newString", "new");
      if (before === undefined || after === undefined) {
        return [];
      }
      return [{ label: `Edit ${index + 1}`, rows: lineDiff(before, after) }];
    });
    if (sections.length === 0) {
      return null;
    }
    return { operation: "Edit file", path, sections };
  }
  return null;
}

function stringProp(record: Record<string, unknown>, ...keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string") {
      return value;
    }
  }
  return undefined;
}

function lineDiff(
  before: string,
  after: string,
): Array<{ kind: "added" | "context" | "removed"; text: string }> {
  const beforeLines = before.split("\n");
  const afterLines = after.split("\n");
  const rows: Array<{ kind: "added" | "context" | "removed"; text: string }> = [];
  const max = Math.max(beforeLines.length, afterLines.length);
  for (let index = 0; index < max; index += 1) {
    const left = beforeLines[index];
    const right = afterLines[index];
    if (left === right && left !== undefined) {
      rows.push({ kind: "context", text: left });
      continue;
    }
    if (left !== undefined) {
      rows.push({ kind: "removed", text: left });
    }
    if (right !== undefined) {
      rows.push({ kind: "added", text: right });
    }
  }
  return rows;
}

function approvalRiskBadges(message: ApprovalPendingMessage, text: string): string[] {
  const lower = text.toLowerCase();
  const badges = new Set<string>();
  if (/\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)\b/.test(lower) || /\brm\s+-rf\b/.test(lower)) {
    badges.add("Destructive delete");
  }
  if (/\bsudo\b/.test(lower)) {
    badges.add("Elevated command");
  }
  if (/\b(chmod|chown)\s+(-r\s+)?(777|root|\/)/.test(lower)) {
    badges.add("Broad permission change");
  }
  if (/\b(curl|wget)\b.*\|\s*(sh|bash|zsh)\b/.test(lower)) {
    badges.add("Network script execution");
  }
  if (message.cwd && /(\s|^)(\/|~\/|\.\.\/)/.test(text) && !text.includes(message.cwd)) {
    badges.add("Path outside cwd");
  }
  return [...badges];
}

function approvalPolicyMetadata(
  metadata: unknown,
): { name?: string; decision: string } | null {
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) {
    return null;
  }
  const policy = (metadata as { onibi_policy?: unknown }).onibi_policy;
  if (!policy || typeof policy !== "object" || Array.isArray(policy)) {
    return null;
  }
  const record = policy as { decision?: unknown; name?: unknown };
  if (typeof record.decision !== "string") {
    return null;
  }
  return {
    decision: record.decision,
    name: typeof record.name === "string" ? record.name : undefined,
  };
}

function formatCountdown(ms: number): string {
  const seconds = Math.ceil(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
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

const headerMetaStyle: CSSProperties = {
  alignItems: "flex-end",
  display: "flex",
  flexDirection: "column",
  gap: 6,
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

const queueStyle: CSSProperties = {
  color: "#9ca3af",
  fontSize: 12,
};

const countdownStyle: CSSProperties = {
  border: "1px solid #f59e0b",
  borderRadius: 999,
  color: "#fbbf24",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 12,
  padding: "3px 8px",
};

const riskWrapStyle: CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: 8,
  marginBottom: 12,
};

const riskBadgeStyle: CSSProperties = {
  background: "rgba(251, 191, 36, 0.12)",
  border: "1px solid rgba(251, 191, 36, 0.42)",
  borderRadius: 999,
  color: "#fbbf24",
  fontSize: 12,
  fontWeight: 700,
  padding: "4px 9px",
};

const policyStyle: CSSProperties = {
  background: "#111827",
  border: "1px solid #4b5563",
  borderRadius: 6,
  color: "#d1d5db",
  fontSize: 12,
  marginBottom: 12,
  padding: "8px 10px",
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

const filePreviewStyle: CSSProperties = {
  background: "#030712",
  border: "1px solid #1f2937",
  borderRadius: 6,
  display: "grid",
  gap: 10,
  maxHeight: 340,
  overflow: "auto",
  padding: 12,
};

const filePreviewHeaderStyle: CSSProperties = {
  alignItems: "center",
  color: "#f9fafb",
  display: "flex",
  flexWrap: "wrap",
  fontSize: 13,
  fontWeight: 700,
  gap: 8,
};

const filePreviewSectionStyle: CSSProperties = {
  display: "grid",
  gap: 4,
};

const filePreviewLabelStyle: CSSProperties = {
  color: "#9ca3af",
  fontSize: 12,
};

const filePreviewDiffStyle: CSSProperties = {
  background: "#020617",
  border: "1px solid #1f2937",
  borderRadius: 6,
  color: "#f3f4f6",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 13,
  lineHeight: 1.5,
  margin: 0,
  overflow: "auto",
  whiteSpace: "pre-wrap",
};

const filePreviewLineStyle: CSSProperties = {
  display: "block",
  padding: "1px 8px",
};

const filePreviewAddedStyle: CSSProperties = {
  background: "rgba(16, 185, 129, 0.12)",
  color: "#6ee7b7",
};

const filePreviewRemovedStyle: CSSProperties = {
  background: "rgba(248, 113, 113, 0.12)",
  color: "#fca5a5",
};

const editWrapStyle: CSSProperties = {
  display: "grid",
  gap: 12,
  marginTop: 14,
};

const denyReasonLabelStyle: CSSProperties = {
  color: "#9ca3af",
  display: "grid",
  fontSize: 12,
  gap: 6,
  marginTop: 14,
};

const denyReasonInputStyle: CSSProperties = {
  background: "#030712",
  border: "1px solid #4b5563",
  borderRadius: 6,
  color: "#f9fafb",
  fontSize: 13,
  padding: "8px 10px",
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
