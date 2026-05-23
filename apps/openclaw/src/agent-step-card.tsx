import { AlertTriangle, CheckCircle2, Clock, Terminal } from "lucide-react";
import type { AgentStepEvent } from "./api-client.js";

export function AgentStepCard(props: { readonly event: AgentStepEvent }) {
  const event = props.event;
  const denied = event.status === "denied" || event.policyDecision?.action === "deny";
  const Icon = denied ? AlertTriangle : event.status === "succeeded" ? CheckCircle2 : Clock;
  return (
    <article className={denied ? "agent-step-card agent-step-card-denied" : "agent-step-card"}>
      <header>
        <span className="agent-step-index">{event.chainIndex + 1}</span>
        <Terminal size={16} />
        <strong>{event.toolName}</strong>
        <span>{event.hookEvent}</span>
        <Icon size={16} />
      </header>
      <div className="agent-step-meta">
        <span>{event.status}</span>
        <span>{event.sourceAgent}</span>
        {event.classification ? <span>{event.classification}</span> : null}
      </div>
      <div className="agent-step-grid">
        <pre>{JSON.stringify(event.args, null, 2)}</pre>
        <pre>{JSON.stringify(event.result ?? {}, null, 2)}</pre>
      </div>
      <footer>{event.prevEventHash}</footer>
    </article>
  );
}
