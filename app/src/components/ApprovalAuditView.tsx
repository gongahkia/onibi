import { useMemo, useState } from "react";
import type { ApprovalAuditRecord } from "../lib/approval-audit";
import type { ApprovalDecision } from "../lib/approval-client";
import {
  fetchCheckpointDiff,
  restoreCheckpoint,
} from "../lib/checkpoints";
import { confirmAction } from "../lib/native-dialogs";
import { appQueryClient } from "../lib/query-client";
import {
  queryKeys,
  useApprovalHistoryQuery,
  useCheckpointsQuery,
} from "../lib/queries";
import { useSessionStore } from "../lib/sessions";
import type { CheckpointDiff, CheckpointRecord } from "../lib/contracts/generated";
import { DiffViewer } from "./DiffViewer";

type ApprovalFilter = "all" | ApprovalDecision | "edited";
type ApprovalAggregate = {
  tool: string;
  total: number;
  allow: number;
  deny: number;
  edited: number;
};

const FILTERS: Array<{ id: ApprovalFilter; label: string }> = [
  { id: "all", label: "All" },
  { id: "allow", label: "Allowed" },
  { id: "deny", label: "Denied" },
  { id: "edited", label: "Edited" },
];

function formatTime(timestamp: number | null | undefined): string {
  if (!timestamp) {
    return "pending";
  }
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(timestamp);
}

function inputText(input: unknown): string {
  if (isRecord(input) && typeof input.command === "string") {
    return input.command;
  }
  return JSON.stringify(input, null, 2);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function uniqueSorted(values: string[]): string[] {
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b));
}

function dateBoundary(value: string, endOfDay: boolean): number | null {
  if (!value) {
    return null;
  }
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) {
    return null;
  }
  return new Date(
    year,
    month - 1,
    day,
    endOfDay ? 23 : 0,
    endOfDay ? 59 : 0,
    endOfDay ? 59 : 0,
    endOfDay ? 999 : 0,
  ).getTime();
}

function searchable(record: ApprovalAuditRecord, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return [
    record.agent,
    record.tool,
    record.cwd,
    record.decision,
    record.reason,
    record.decided_by,
    inputText(record.input),
    record.updatedInput ? inputText(record.updatedInput) : "",
  ]
    .join(" ")
    .toLowerCase()
    .includes(needle);
}

function auditErrorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  if (/failed to fetch/i.test(message)) {
    return "Approval daemon unavailable. Start Onibi or the headless daemon to load history.";
  }
  return message;
}

function aggregateByTool(records: ApprovalAuditRecord[]): ApprovalAggregate[] {
  const aggregates = new Map<string, ApprovalAggregate>();
  for (const record of records) {
    const tool = record.tool || "Unknown";
    const aggregate =
      aggregates.get(tool) ??
      ({
        tool,
        total: 0,
        allow: 0,
        deny: 0,
        edited: 0,
      } satisfies ApprovalAggregate);
    aggregate.total += 1;
    if (record.decision === "allow") {
      aggregate.allow += 1;
    }
    if (record.decision === "deny") {
      aggregate.deny += 1;
    }
    if (record.updatedInput) {
      aggregate.edited += 1;
    }
    aggregates.set(tool, aggregate);
  }
  return Array.from(aggregates.values()).sort((a, b) => {
    if (b.total !== a.total) {
      return b.total - a.total;
    }
    return a.tool.localeCompare(b.tool);
  });
}

export function ApprovalAuditView() {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ApprovalFilter>("all");
  const [agentFilter, setAgentFilter] = useState("all");
  const [toolFilter, setToolFilter] = useState("all");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [checkpointDiff, setCheckpointDiff] = useState<CheckpointDiff | null>(null);
  const [checkpointError, setCheckpointError] = useState<string | null>(null);
  const diffViewMode = useSessionStore((state) => state.settings.diffViewMode);
  const {
    data: records = [],
    error,
    isLoading: loading,
  } = useApprovalHistoryQuery({ limit: 500 });
  const { data: checkpoints = [] } = useCheckpointsQuery({ limit: 500 });

  const agentOptions = useMemo(
    () => uniqueSorted(records.map((record) => record.agent)),
    [records],
  );
  const toolOptions = useMemo(
    () => uniqueSorted(records.map((record) => record.tool)),
    [records],
  );
  const fromMillis = useMemo(() => dateBoundary(fromDate, false), [fromDate]);
  const toMillis = useMemo(() => dateBoundary(toDate, true), [toDate]);
  const visible = useMemo(
    () =>
      records.filter((record) => {
        if (agentFilter !== "all" && record.agent !== agentFilter) {
          return false;
        }
        if (toolFilter !== "all" && record.tool !== toolFilter) {
          return false;
        }
        if (fromMillis !== null && record.created_at < fromMillis) {
          return false;
        }
        if (toMillis !== null && record.created_at > toMillis) {
          return false;
        }
        if (filter === "edited" && !record.updatedInput) {
          return false;
        }
        if (filter === "allow" && record.decision !== "allow") {
          return false;
        }
        if (filter === "deny" && record.decision !== "deny") {
          return false;
        }
        return searchable(record, query);
      }),
    [agentFilter, filter, fromMillis, query, records, toMillis, toolFilter],
  );

  const totals = useMemo(() => {
    return {
      all: visible.length,
      allow: visible.filter((record) => record.decision === "allow").length,
      deny: visible.filter((record) => record.decision === "deny").length,
      edited: visible.filter((record) => record.updatedInput).length,
    };
  }, [visible]);
  const aggregates = useMemo(() => aggregateByTool(visible), [visible]);
  const checkpointByApproval = useMemo(() => {
    return new Map(checkpoints.map((checkpoint) => [checkpoint.approvalId, checkpoint]));
  }, [checkpoints]);
  const filtersActive =
    query.trim() !== "" ||
    filter !== "all" ||
    agentFilter !== "all" ||
    toolFilter !== "all" ||
    fromDate !== "" ||
    toDate !== "";

  function exportJsonl() {
    const jsonl = visible.map((record) => JSON.stringify(record)).join("\n");
    if (typeof URL.createObjectURL !== "function") {
      void navigator.clipboard?.writeText(jsonl);
      return;
    }
    const blob = new Blob([`${jsonl}\n`], { type: "application/x-ndjson" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `onibi-approvals-${Date.now()}.jsonl`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  function resetFilters() {
    setQuery("");
    setFilter("all");
    setAgentFilter("all");
    setToolFilter("all");
    setFromDate("");
    setToDate("");
  }

  async function showCheckpointDiff(checkpoint: CheckpointRecord) {
    setCheckpointError(null);
    try {
      setCheckpointDiff(await fetchCheckpointDiff(checkpoint.approvalId));
    } catch (caught) {
      setCheckpointError(caught instanceof Error ? caught.message : String(caught));
    }
  }

  async function restoreBeforeTurn(checkpoint: CheckpointRecord) {
    const confirmed = await confirmAction(
      `Restore workspace to before approval ${checkpoint.approvalId}?`,
      {
        okLabel: "Restore",
        title: "Restore Checkpoint",
      },
    );
    if (!confirmed) {
      return;
    }
    setCheckpointError(null);
    try {
      await restoreCheckpoint(checkpoint.approvalId);
      await appQueryClient.invalidateQueries({ queryKey: queryKeys.checkpoints({ limit: 500 }) });
    } catch (caught) {
      setCheckpointError(caught instanceof Error ? caught.message : String(caught));
    }
  }

  return (
    <section className="session-history-view" aria-label="Approval audit log">
      <div className="activity-toolbar">
        <input
          className="settings-input"
          aria-label="Search approval audit log"
          placeholder="Search approvals, tools, commands, reasons"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="activity-actions">
          <button
            type="button"
            className="text-button"
            disabled={visible.length === 0}
            onClick={exportJsonl}
          >
            Export JSONL
          </button>
        </div>
      </div>
      <div className="approval-audit-filter-grid">
        <label>
          <span>Agent</span>
          <select
            className="settings-select"
            aria-label="Filter approvals by agent"
            value={agentFilter}
            onChange={(event) => setAgentFilter(event.target.value)}
          >
            <option value="all">All agents</option>
            {agentOptions.map((agent) => (
              <option key={agent} value={agent}>
                {agent}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>Tool</span>
          <select
            className="settings-select"
            aria-label="Filter approvals by tool"
            value={toolFilter}
            onChange={(event) => setToolFilter(event.target.value)}
          >
            <option value="all">All tools</option>
            {toolOptions.map((tool) => (
              <option key={tool} value={tool}>
                {tool}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>From</span>
          <input
            className="settings-input"
            aria-label="Filter approvals from date"
            type="date"
            value={fromDate}
            onChange={(event) => setFromDate(event.target.value)}
          />
        </label>
        <label>
          <span>To</span>
          <input
            className="settings-input"
            aria-label="Filter approvals to date"
            type="date"
            value={toDate}
            onChange={(event) => setToDate(event.target.value)}
          />
        </label>
        <button
          type="button"
          className="text-button"
          disabled={!filtersActive}
          onClick={resetFilters}
        >
          Reset
        </button>
      </div>
      <div className="activity-filter-row" role="tablist" aria-label="Approval filter">
        {FILTERS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={filter === item.id ? "active" : ""}
            aria-selected={filter === item.id}
            role="tab"
            onClick={() => setFilter(item.id)}
          >
            {item.label}
          </button>
        ))}
      </div>
      <div className="history-session-status-row">
        <span>{totals.all} shown</span>
        <span>{totals.allow} allowed</span>
        <span>{totals.deny} denied</span>
        <span>{totals.edited} edited</span>
      </div>
      {aggregates.length > 0 ? (
        <div className="approval-aggregate-row" aria-label="Approval aggregates by tool">
          {aggregates.map((aggregate) => (
            <span className="approval-aggregate-chip" key={aggregate.tool}>
              <strong>{aggregate.tool}</strong>
              {aggregate.total} total · {aggregate.allow} allowed · {aggregate.deny} denied ·{" "}
              {aggregate.edited} edited
            </span>
          ))}
        </div>
      ) : null}
      {loading ? <div className="source-control-empty">Loading approvals...</div> : null}
      {error ? <div className="tree-error">{auditErrorMessage(error)}</div> : null}
      {checkpointError ? <div className="tree-error">{checkpointError}</div> : null}
      {!loading && !error && visible.length === 0 ? (
        <div className="source-control-empty">No approvals match this filter.</div>
      ) : null}
      {visible.map((record) => (
        <ApprovalAuditRow
          key={record.approval_id}
          record={record}
          checkpoint={checkpointByApproval.get(record.approval_id) ?? null}
          checkpointDiff={
            checkpointDiff?.approvalId === record.approval_id ? checkpointDiff : null
          }
          diffViewMode={diffViewMode}
          onRestoreCheckpoint={(checkpoint) => void restoreBeforeTurn(checkpoint)}
          onShowCheckpointDiff={(checkpoint) => void showCheckpointDiff(checkpoint)}
        />
      ))}
    </section>
  );
}

function ApprovalAuditRow({
  checkpoint,
  checkpointDiff,
  diffViewMode,
  onRestoreCheckpoint,
  onShowCheckpointDiff,
  record,
}: {
  checkpoint: CheckpointRecord | null;
  checkpointDiff: CheckpointDiff | null;
  diffViewMode: "unified" | "side-by-side";
  onRestoreCheckpoint: (checkpoint: CheckpointRecord) => void;
  onShowCheckpointDiff: (checkpoint: CheckpointRecord) => void;
  record: ApprovalAuditRecord;
}) {
  const proposed = inputText(record.input);
  const updated = record.updatedInput ? inputText(record.updatedInput) : "";
  return (
    <article className={`history-event command-${record.decision ?? "running"}`}>
      <div className="history-event-time">{formatTime(record.created_at)}</div>
      <div className="history-event-body">
        <div className="history-event-label">
          {record.agent} · {record.tool}
        </div>
        <div className="history-event-summary">
          {record.decision ?? "pending"}
          {record.decided_by ? ` by ${record.decided_by}` : ""}
          {record.decided_at ? ` at ${formatTime(record.decided_at)}` : ""}
        </div>
        <div className="history-event-meta">{record.cwd || "No working directory"}</div>
        {updated ? (
          <ApprovalDiff proposed={proposed} updated={updated} />
        ) : (
          <pre className="history-output">{proposed}</pre>
        )}
        {record.reason ? (
          <div className="trigger-action-row">
            <span>Reason: {record.reason}</span>
          </div>
        ) : null}
        {checkpoint ? (
          <div className="trigger-action-row approval-checkpoint-row">
            <span>
              Checkpoint {checkpoint.postRef ? "pre/post" : "pre only"}
              {checkpoint.error ? ` · ${checkpoint.error}` : ""}
            </span>
            <button
              type="button"
              className="text-button"
              disabled={!checkpoint.postRef}
              onClick={() => onShowCheckpointDiff(checkpoint)}
            >
              Show diff
            </button>
            <button
              type="button"
              className="text-button danger"
              onClick={() => onRestoreCheckpoint(checkpoint)}
            >
              Restore before turn
            </button>
          </div>
        ) : null}
        {checkpointDiff ? (
          <CheckpointDiffPreview diff={checkpointDiff} mode={diffViewMode} />
        ) : null}
      </div>
    </article>
  );
}

function CheckpointDiffPreview({
  diff,
  mode,
}: {
  diff: CheckpointDiff;
  mode: "unified" | "side-by-side";
}) {
  if (!diff.postRef) {
    return <div className="source-control-empty">Post-turn checkpoint not available.</div>;
  }
  if (diff.files.length === 0) {
    return <div className="source-control-empty">No file changes in this checkpoint.</div>;
  }
  return (
    <div className="approval-checkpoint-diff" aria-label="Checkpoint diff">
      {diff.files.slice(0, 3).map((file) => (
        <DiffViewer key={file.path} diff={file} mode={mode} />
      ))}
      {diff.files.length > 3 ? (
        <div className="history-event-meta">{diff.files.length - 3} more files hidden</div>
      ) : null}
    </div>
  );
}

function ApprovalDiff({ proposed, updated }: { proposed: string; updated: string }) {
  const rows = diffRows(proposed, updated);
  return (
    <div className="approval-diff" aria-label="Proposed to final input diff">
      <div className="approval-diff-heading">
        <span>Proposed input</span>
        <span>Final input</span>
      </div>
      <pre className="approval-diff-body">
        {rows.map((row, index) => (
          <span className={`approval-diff-line ${row.kind}`} key={`${row.kind}-${index}`}>
            {row.kind === "removed" ? "- " : row.kind === "added" ? "+ " : "  "}
            {row.text || " "}
          </span>
        ))}
      </pre>
    </div>
  );
}

function diffRows(
  proposed: string,
  updated: string,
): Array<{ kind: "added" | "context" | "removed"; text: string }> {
  const proposedLines = proposed.split("\n");
  const updatedLines = updated.split("\n");
  const rows: Array<{ kind: "added" | "context" | "removed"; text: string }> = [];
  const max = Math.max(proposedLines.length, updatedLines.length);
  for (let index = 0; index < max; index += 1) {
    const before = proposedLines[index];
    const after = updatedLines[index];
    if (before === after && before !== undefined) {
      rows.push({ kind: "context", text: before });
      continue;
    }
    if (before !== undefined) {
      rows.push({ kind: "removed", text: before });
    }
    if (after !== undefined) {
      rows.push({ kind: "added", text: after });
    }
  }
  return rows;
}
