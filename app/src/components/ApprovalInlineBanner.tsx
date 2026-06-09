import { useEffect, useMemo, useRef, useState } from "react";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { rust } from "@codemirror/lang-rust";
import { yaml } from "@codemirror/lang-yaml";
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { tags as highlightTags } from "@lezer/highlight";
import {
  decideApproval,
  storedApprovalPort,
  subscribeApprovalEvents,
  type ApprovalPendingMessage,
  type ApprovalRealtimeMessage,
} from "../lib/approval-client";
import { useSessionStore } from "../lib/sessions";
import { requestInformationalAttention } from "../lib/window-attention";
import { ProseComposer } from "./composer/ProseComposer";

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
  const [now, setNow] = useState(Date.now());
  const attentionSeen = useRef<Set<string>>(new Set());
  const sectionRef = useRef<HTMLElement | null>(null);

  const pending = queue[0] ?? null;

  useEffect(() => {
    function handleFocus() {
      sectionRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }
    window.addEventListener("onibi:focus-approval-banner", handleFocus);
    return () =>
      window.removeEventListener("onibi:focus-approval-banner", handleFocus);
  }, []);

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
          requestApprovalAttention(message, attentionSeen.current);
          setQueue((items) => {
            const exists = items.some((item) => item.approval_id === message.approval_id);
            if (items.length === 0) {
              setEditing(false);
              setEditValue(editTextFor(message));
              setDenyReason("");
            }
            const next = exists
              ? items.map((item) =>
                  item.approval_id === message.approval_id ? message : item,
                )
              : [...items, message];
            return sortApprovals(next);
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
              setEditValue(next[0] ? editTextFor(next[0]) : "");
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
      setEditValue(editTextFor(initialPending));
      setDenyReason("");
    }
  }, [initialPending]);

  useEffect(() => {
    if (pending && !editing) {
      setEditValue(editTextFor(pending));
    }
  }, [editing, pending]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const formattedInput = useMemo(
    () => (pending ? formatInput(pending) : ""),
    [pending],
  );
  const riskBadges = useMemo(
    () => (pending ? approvalRiskBadges(pending, formattedInput) : []),
    [formattedInput, pending],
  );
  const policy = useMemo(
    () => (pending ? approvalPolicyMetadata(pending.metadata) : null),
    [pending],
  );
  const canEditInput = pending ? approvalSupportsUpdatedInput(pending) : false;
  const timeRemaining = pending?.expires_at
    ? Math.max(0, pending.expires_at - now)
    : null;
  const showCountdown = typeof timeRemaining === "number" && timeRemaining <= 60_000;

  if (!pending) {
    return toast ? (
      <div className="approval-banner approval-banner-toast" role="status">
        {toast}
      </div>
    ) : null;
  }
  const proseEdit = proseEditDescriptor(pending);

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
      setQueue((items) => {
        const next = items.filter((item) => item.approval_id !== pending.approval_id);
        setEditValue(next[0] ? editTextFor(next[0]) : "");
        setDenyReason("");
        return next;
      });
      setEditing(false);
      setError(null);
      setToast(null);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "approval request failed");
    }
  };

  return (
    <section
      ref={sectionRef}
      className="approval-banner"
      role="region"
      aria-label="Approval request"
      data-tool={pending.tool}
    >
      <header className="approval-banner-header">
        <span className="approval-banner-agent">{pending.agent}</span>
        <span className="approval-banner-tool">{pending.tool}</span>
        {pending.cwd ? (
          <span className="approval-banner-cwd" title={pending.cwd}>
            {pending.cwd}
          </span>
        ) : null}
        {queue.length > 1 ? (
          <span className="approval-banner-queue">
            1 of {queue.length}
          </span>
        ) : null}
        {showCountdown ? (
          <span className="approval-banner-countdown">
            denies in {formatCountdown(timeRemaining ?? 0)}
          </span>
        ) : null}
      </header>
      {riskBadges.length > 0 ? (
        <div className="approval-banner-risk-list" aria-label="Approval risk indicators">
          {riskBadges.map((badge) => (
            <span key={badge} className="approval-banner-risk-badge">
              {badge}
            </span>
          ))}
        </div>
      ) : null}
      {policy ? (
        <div className="approval-banner-policy" aria-label="Matched approval policy">
          Policy {policy.name ? `"${policy.name}"` : "matched"} · {policy.decision}
        </div>
      ) : null}

      {editing && canEditInput ? (
        <div className="approval-banner-edit">
          {proseEdit ? (
            <ProseComposer
              ariaLabel="Edited tool input"
              className="approval-banner-textarea approval-banner-prose"
              value={editValue}
              onChange={setEditValue}
            />
          ) : (
            <textarea
              aria-label="Edited tool input"
              value={editValue}
              onChange={(event) => setEditValue(event.target.value)}
              className="approval-banner-textarea"
              rows={4}
            />
          )}
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
              className="text-button primary approval-allow"
              onClick={() => void submit("allow", updatedInputFor(pending, editValue))}
            >
              Edit & Allow
            </button>
          </div>
        </div>
      ) : (
        <>
          <ApprovalPayloadPreview message={pending} fallback={formattedInput} />
          <label className="approval-banner-deny-label">
            <span>Deny reason (optional)</span>
            <ProseComposer
              ariaLabel="Deny reason"
              value={denyReason}
              placeholder="Why this should not run"
              className="approval-banner-deny-input"
              onChange={setDenyReason}
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
                  setEditValue(proseEdit?.value ?? formattedInput);
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

function requestApprovalAttention(
  message: ApprovalPendingMessage,
  seen: Set<string>,
): void {
  if (seen.has(message.approval_id)) {
    return;
  }
  seen.add(message.approval_id);
  const suppressed = shouldSuppressApprovalAttention(message);
  if (typeof window !== "undefined") {
    window.dispatchEvent(
      new CustomEvent("onibi:approval-attention", {
        detail: {
          approvalId: message.approval_id,
          escalate: !suppressed,
          sessionId: message.session_id,
        },
      }),
    );
  }
  if (suppressed) {
    return;
  }
  void requestInformationalAttention();
}

function shouldSuppressApprovalAttention(message: ApprovalPendingMessage): boolean {
  const state = useSessionStore.getState();
  return (
    state.settings.suppressForegroundTabNotifications &&
    state.selectedFile === null &&
    state.activeSessionId === message.session_id
  );
}

type ApprovalCodeLanguage =
  | "shell"
  | "json"
  | "typescript"
  | "javascript"
  | "css"
  | "html"
  | "markdown"
  | "python"
  | "rust"
  | "yaml"
  | "text";

type FileEditPreview =
  | {
      kind: "write";
      operation: string;
      path?: string;
      content: string;
      language: ApprovalCodeLanguage;
    }
  | {
      kind: "edit";
      operation: string;
      path?: string;
      edits: Array<{
        label: string;
        before: string;
        after: string;
        language: ApprovalCodeLanguage;
      }>;
    };

function ApprovalPayloadPreview({
  fallback,
  message,
}: {
  fallback: string;
  message: ApprovalPendingMessage;
}) {
  const [page, setPage] = useState(0);
  useEffect(() => {
    setPage(0);
  }, [message.approval_id]);
  if (message.tool === "Bash") {
    return (
      <div className="approval-banner-preview-block">
        <div className="approval-banner-preview-header">Shell command</div>
        <ApprovalCodePreview
          ariaLabel="Bash command preview"
          language="shell"
          value={fallback}
        />
      </div>
    );
  }
  const filePreview = fileEditPreview(message);
  if (!filePreview) {
    return (
      <div className="approval-banner-preview-block">
        <div className="approval-banner-preview-header">Tool input</div>
        <ApprovalCodePreview
          ariaLabel="JSON approval payload preview"
          language="json"
          value={fallback}
        />
      </div>
    );
  }
  if (filePreview.kind === "write") {
    return (
      <div className="approval-banner-file-preview" aria-label={`${message.tool} file change preview`}>
        <div className="approval-banner-file-preview-header">
          <span>{filePreview.operation}</span>
          {filePreview.path ? <code>{filePreview.path}</code> : null}
        </div>
        <div className="approval-banner-file-preview-section">
          <div className="approval-banner-file-preview-label">New content</div>
          <ApprovalCodePreview
            ariaLabel="Write file content preview"
            language={filePreview.language}
            value={filePreview.content}
          />
        </div>
      </div>
    );
  }
  const pageSize = 5;
  const totalPages = Math.max(1, Math.ceil(filePreview.edits.length / pageSize));
  const safePage = Math.min(page, totalPages - 1);
  const visibleEdits = filePreview.edits.slice(
    safePage * pageSize,
    safePage * pageSize + pageSize,
  );
  return (
    <div className="approval-banner-file-preview" aria-label={`${message.tool} file change preview`}>
      <div className="approval-banner-file-preview-header">
        <span>{filePreview.operation}</span>
        {filePreview.path ? <code>{filePreview.path}</code> : null}
      </div>
      {visibleEdits.map((edit) => (
        <div className="approval-banner-file-preview-section" key={edit.label}>
          <div className="approval-banner-file-preview-label">{edit.label}</div>
          <div className="approval-banner-side-by-side">
            <div className="approval-banner-side-pane">
              <div className="approval-banner-side-label">Before</div>
              <ApprovalCodePreview
                ariaLabel={`${edit.label} before preview`}
                language={edit.language}
                value={edit.before}
              />
            </div>
            <div className="approval-banner-side-pane">
              <div className="approval-banner-side-label">After</div>
              <ApprovalCodePreview
                ariaLabel={`${edit.label} after preview`}
                language={edit.language}
                value={edit.after}
              />
            </div>
          </div>
        </div>
      ))}
      {totalPages > 1 ? (
        <div className="approval-banner-preview-pager" aria-label="MultiEdit preview pages">
          <button
            type="button"
            className="approval-banner-preview-page-button"
            disabled={safePage === 0}
            onClick={() => setPage((current) => Math.max(0, current - 1))}
          >
            Previous
          </button>
          <span>
            Page {safePage + 1} of {totalPages}
          </span>
          <button
            type="button"
            className="approval-banner-preview-page-button"
            disabled={safePage >= totalPages - 1}
            onClick={() => setPage((current) => Math.min(totalPages - 1, current + 1))}
          >
            Next
          </button>
        </div>
      ) : null}
    </div>
  );
}

function ApprovalCodePreview({
  ariaLabel,
  language,
  value,
}: {
  ariaLabel: string;
  language: ApprovalCodeLanguage;
  value: string;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!hostRef.current) {
      return;
    }
    hostRef.current.textContent = "";
    const view = new EditorView({
      parent: hostRef.current,
      state: EditorState.create({
        doc: value,
        extensions: approvalCodeExtensions(language),
      }),
    });
    return () => view.destroy();
  }, [language, value]);
  return (
    <div
      ref={hostRef}
      className="approval-banner-code-preview"
      aria-label={ariaLabel}
      role="region"
    />
  );
}

const approvalHighlightStyle = HighlightStyle.define([
  { tag: highlightTags.keyword, color: "var(--accent)" },
  { tag: [highlightTags.name, highlightTags.deleted], color: "var(--fg-0)" },
  { tag: [highlightTags.variableName, highlightTags.propertyName], color: "var(--fg-0)" },
  {
    tag: [highlightTags.function(highlightTags.variableName), highlightTags.labelName],
    color: "var(--accent-2)",
  },
  { tag: [highlightTags.string, highlightTags.special(highlightTags.string)], color: "var(--success)" },
  { tag: [highlightTags.number, highlightTags.bool, highlightTags.null], color: "var(--attention)" },
  { tag: [highlightTags.comment, highlightTags.lineComment, highlightTags.blockComment], color: "var(--fg-2)" },
  { tag: [highlightTags.atom, highlightTags.meta], color: "var(--fg-1)" },
  { tag: [highlightTags.typeName, highlightTags.className], color: "var(--accent-2)" },
  { tag: highlightTags.invalid, color: "var(--danger)" },
]);

const approvalCodeTheme = EditorView.theme({
  "&": {
    height: "auto",
    color: "var(--fg-0)",
    backgroundColor: "var(--bg-0)",
    fontSize: "var(--font-size-editor)",
  },
  ".cm-scroller": {
    maxHeight: "220px",
    overflow: "auto",
    fontFamily: "var(--font-mono)",
    lineHeight: "1.45",
  },
  ".cm-content": {
    minHeight: "0",
    padding: "8px 0",
    caretColor: "transparent",
    fontFeatureSettings: "\"calt\" off",
  },
  ".cm-line": {
    padding: "0 10px",
  },
  ".cm-gutters": {
    color: "var(--fg-2)",
    backgroundColor: "var(--bg-1)",
    borderRight: "1px solid var(--border)",
  },
  "&.cm-focused": {
    outline: "none",
  },
  ".cm-selectionBackground, ::selection": {
    backgroundColor: "var(--terminal-selection)",
  },
});

function approvalCodeExtensions(language: ApprovalCodeLanguage): Extension[] {
  const languageExtension = languageExtensionFor(language);
  return [
    EditorState.readOnly.of(true),
    EditorView.editable.of(false),
    EditorView.lineWrapping,
    approvalCodeTheme,
    syntaxHighlighting(approvalHighlightStyle),
    ...(languageExtension ? [languageExtension] : []),
  ];
}

function languageExtensionFor(language: ApprovalCodeLanguage): Extension | null {
  switch (language) {
    case "shell":
      return StreamLanguage.define(shell);
    case "json":
      return json();
    case "typescript":
      return javascript({ typescript: true, jsx: true });
    case "javascript":
      return javascript({ jsx: true });
    case "css":
      return css();
    case "html":
      return html();
    case "markdown":
      return markdown();
    case "python":
      return python();
    case "rust":
      return rust();
    case "yaml":
      return yaml();
    default:
      return null;
  }
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
  if (typeof message.input === "string") {
    return message.input;
  }
  return JSON.stringify(message.input, null, 2);
}

function updatedInputFor(message: ApprovalPendingMessage, value: string): unknown {
  const proseEdit = proseEditDescriptor(message);
  if (proseEdit) {
    if (proseEdit.kind === "raw") {
      return value;
    }
    return { ...(message.input as Record<string, unknown>), [proseEdit.field]: value };
  }
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

type ProseEditDescriptor =
  | { kind: "raw"; value: string }
  | { kind: "field"; field: string; value: string };

function editTextFor(message: ApprovalPendingMessage): string {
  return proseEditDescriptor(message)?.value ?? formatInput(message);
}

function proseEditDescriptor(message: ApprovalPendingMessage): ProseEditDescriptor | null {
  if (message.tool === "Bash" || fileEditPreview(message)) {
    return null;
  }
  if (typeof message.input === "string") {
    return { kind: "raw", value: message.input };
  }
  if (!message.input || typeof message.input !== "object" || Array.isArray(message.input)) {
    return null;
  }
  const input = message.input as Record<string, unknown>;
  for (const field of ["prompt", "message", "text", "instructions", "reason", "content"]) {
    if (typeof input[field] === "string") {
      return { kind: "field", field, value: input[field] };
    }
  }
  return null;
}

function fileEditPreview(message: ApprovalPendingMessage): FileEditPreview | null {
  if (!message.input || typeof message.input !== "object" || Array.isArray(message.input)) {
    return null;
  }
  const input = message.input as Record<string, unknown>;
  const tool = message.tool.toLowerCase();
  const path = stringProp(input, "file_path", "path", "filePath");
  const language = languageForPath(path);
  if (tool === "write") {
    const content = stringProp(input, "content", "text", "new_string", "newString");
    if (content === undefined) {
      return null;
    }
    return {
      kind: "write",
      operation: "Write file",
      path,
      content,
      language,
    };
  }
  if (tool === "edit") {
    const before = stringProp(input, "old_string", "oldString", "old");
    const after = stringProp(input, "new_string", "newString", "new");
    if (before === undefined || after === undefined) {
      return null;
    }
    return {
      kind: "edit",
      operation: "Edit file",
      path,
      edits: [{ label: "Edit 1 of 1", before, after, language }],
    };
  }
  if (tool === "multiedit") {
    const edits = Array.isArray(input.edits) ? input.edits : [];
    const previewEdits = edits.flatMap((edit, index) => {
      if (!edit || typeof edit !== "object" || Array.isArray(edit)) {
        return [];
      }
      const record = edit as Record<string, unknown>;
      const before = stringProp(record, "old_string", "oldString", "old");
      const after = stringProp(record, "new_string", "newString", "new");
      if (before === undefined || after === undefined) {
        return [];
      }
      return [
        {
          label: `Edit ${index + 1} of ${edits.length}`,
          before,
          after,
          language,
        },
      ];
    });
    if (previewEdits.length === 0) {
      return null;
    }
    return { kind: "edit", operation: "Edit file", path, edits: previewEdits };
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

function languageForPath(path: string | undefined): ApprovalCodeLanguage {
  const extension = path?.split(".").pop()?.toLowerCase();
  switch (extension) {
    case "sh":
    case "bash":
    case "zsh":
      return "shell";
    case "json":
      return "json";
    case "ts":
    case "tsx":
      return "typescript";
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return "javascript";
    case "css":
      return "css";
    case "html":
    case "htm":
      return "html";
    case "md":
    case "mdx":
      return "markdown";
    case "py":
      return "python";
    case "rs":
      return "rust";
    case "yaml":
    case "yml":
      return "yaml";
    default:
      return "text";
  }
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

function approvalSupportsUpdatedInput(message: ApprovalPendingMessage): boolean {
  if (!message.metadata || typeof message.metadata !== "object") {
    return true;
  }
  const metadata = message.metadata as Record<string, unknown>;
  return metadata.supportsUpdatedInput !== false;
}

function formatCountdown(ms: number): string {
  const seconds = Math.ceil(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}
