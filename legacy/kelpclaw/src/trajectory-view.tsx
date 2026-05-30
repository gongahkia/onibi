import { Anchor, RefreshCw } from "lucide-react";
import type { AgentRunAuditEvent, AgentRunRecord, AgentStepEvent } from "./api-client.js";
import { AgentStepCard } from "./agent-step-card.js";

export function TrajectoryView(props: {
  readonly runs: readonly AgentRunRecord[];
  readonly selectedRunId: string | null;
  readonly onSelectRun: (runId: string) => void;
  readonly onRefresh: () => void;
  readonly onAnchorRun: (runId: string) => void;
  readonly onApproveEvent: (runId: string, event: AgentStepEvent) => void;
  readonly onDenyEvent: (runId: string, event: AgentStepEvent) => void;
  readonly notice?: string | null | undefined;
}) {
  const selectedRun = props.runs.find((run) => run.id === props.selectedRunId) ?? props.runs[0];
  const latestAnchor = selectedRun ? latestAuditAnchor(selectedRun.auditEvents) : undefined;
  return (
    <section className="trajectory-view" aria-label="Agent trajectory">
      <aside className="trajectory-run-list">
        <div className="trajectory-panel-header">
          <strong>Agent Runs</strong>
          <button type="button" className="icon-button" title="Refresh" onClick={props.onRefresh}>
            <RefreshCw size={16} />
          </button>
        </div>
        {props.runs.map((run) => (
          <button
            key={run.id}
            type="button"
            className={run.id === selectedRun?.id ? "trajectory-run-active" : ""}
            onClick={() => props.onSelectRun(run.id)}
          >
            <strong>{run.title ?? run.id}</strong>
            <span>
              {run.sourceAgent} · {run.status} · {run.events.length}
            </span>
          </button>
        ))}
      </aside>
      <div className="trajectory-steps">
        {selectedRun ? (
          <>
            <div className="trajectory-action-bar">
              <div>
                <strong>{selectedRun.title ?? selectedRun.id}</strong>
                <span>
                  {latestAnchor
                    ? `${String(latestAnchor.metadata?.chainHead ?? "").slice(0, 24)} · ${String(
                        latestAnchor.metadata?.externalAnchorStatus ?? "local"
                      )}`
                    : "not anchored"}
                </span>
              </div>
              <button type="button" onClick={() => props.onAnchorRun(selectedRun.id)}>
                <Anchor size={16} />
                Anchor
              </button>
            </div>
            {props.notice ? <div className="trajectory-notice">{props.notice}</div> : null}
            {selectedRun.auditEvents.length > 0 ? (
              <div className="trajectory-audit-strip">
                {selectedRun.auditEvents.slice(-4).map((event) => (
                  <span key={event.id}>{event.action}</span>
                ))}
              </div>
            ) : null}
            {selectedRun.events.length > 0 ? (
              selectedRun.events.map((event) => (
                <AgentStepCard
                  key={event.id}
                  event={event}
                  approvalStatus={approvalStatus(selectedRun.auditEvents, event.id)}
                  onApprove={(candidate) => props.onApproveEvent(selectedRun.id, candidate)}
                  onDeny={(candidate) => props.onDenyEvent(selectedRun.id, candidate)}
                />
              ))
            ) : (
              <div className="empty-state">No recorded steps.</div>
            )}
          </>
        ) : (
          <div className="empty-state">No agent runs recorded.</div>
        )}
      </div>
    </section>
  );
}

function latestAuditAnchor(
  auditEvents: readonly AgentRunAuditEvent[]
): AgentRunAuditEvent | undefined {
  return [...auditEvents].reverse().find((event) => event.action === "audit.anchored");
}

function approvalStatus(
  auditEvents: readonly AgentRunAuditEvent[],
  eventId: string
): "approved" | "denied" | undefined {
  for (const auditEvent of auditEvents) {
    if (auditEvent.eventId !== eventId) {
      continue;
    }
    if (auditEvent.action === "policy.approved") {
      return "approved";
    }
    if (auditEvent.metadata?.approvalStatus === "denied") {
      return "denied";
    }
  }
  return undefined;
}
