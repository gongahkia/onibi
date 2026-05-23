import { RefreshCw } from "lucide-react";
import type { AgentRunRecord } from "./api-client.js";
import { AgentStepCard } from "./agent-step-card.js";

export function TrajectoryView(props: {
  readonly runs: readonly AgentRunRecord[];
  readonly selectedRunId: string | null;
  readonly onSelectRun: (runId: string) => void;
  readonly onRefresh: () => void;
}) {
  const selectedRun = props.runs.find((run) => run.id === props.selectedRunId) ?? props.runs[0];
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
          selectedRun.events.length > 0 ? (
            selectedRun.events.map((event) => <AgentStepCard key={event.id} event={event} />)
          ) : (
            <div className="empty-state">No recorded steps.</div>
          )
        ) : (
          <div className="empty-state">No agent runs recorded.</div>
        )}
      </div>
    </section>
  );
}
