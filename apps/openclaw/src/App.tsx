import { useMemo, useState } from "react";
import { Background, Controls, MiniMap, ReactFlow } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { CheckCircle2, GitBranch, Play, RefreshCw, ShieldCheck } from "lucide-react";
import {
  gmailReceiptsToSheetsWorkflowFixture,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import { workflowToEdges, workflowToNodes } from "./workflow-elements.js";
import "./styles.css";

type ValidationState = "pending" | "valid";
type ApprovalState = "pending" | "approved";
type ExecutionState = "idle" | "blocked" | "succeeded";

export function App() {
  const workflow = gmailReceiptsToSheetsWorkflowFixture;
  const nodes = useMemo(() => workflowToNodes(workflow), [workflow]);
  const edges = useMemo(() => workflowToEdges(workflow), [workflow]);
  const [validationState, setValidationState] = useState<ValidationState>("pending");
  const [approvalState, setApprovalState] = useState<ApprovalState>("pending");
  const [executionState, setExecutionState] = useState<ExecutionState>("idle");

  const validation = validateWorkflowSpec(workflow);
  const approvalNodeCount = workflow.nodes.filter((node) => node.kind === "approval").length;

  function validateWorkflow() {
    setValidationState(validation.ok ? "valid" : "pending");
    setExecutionState("idle");
  }

  function approveWorkflow() {
    setApprovalState("approved");
    setExecutionState("idle");
  }

  function runWorkflow() {
    setExecutionState(approvalState === "approved" ? "succeeded" : "blocked");
  }

  function resetWorkflow() {
    setValidationState("pending");
    setApprovalState("pending");
    setExecutionState("idle");
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">KelpClaw</p>
          <h1>OpenClaw</h1>
        </div>
        <div className="topbar-actions" aria-label="Workflow actions">
          <button title="Validate workflow" onClick={validateWorkflow}>
            <ShieldCheck size={18} />
            Validate
          </button>
          <button title="Approve workflow" onClick={approveWorkflow}>
            <CheckCircle2 size={18} />
            Approve
          </button>
          <button title="Run workflow" onClick={runWorkflow}>
            <Play size={18} />
            Run
          </button>
          <button className="icon-button" title="Reset workflow" onClick={resetWorkflow}>
            <RefreshCw size={18} />
          </button>
        </div>
      </header>

      <section className="workspace">
        <aside className="panel planner-panel" aria-label="Workflow summary">
          <div className="panel-heading">
            <GitBranch size={18} />
            <h2>{workflow.name}</h2>
          </div>
          <dl className="metric-grid">
            <div>
              <dt>Nodes</dt>
              <dd>{workflow.nodes.length}</dd>
            </div>
            <div>
              <dt>Edges</dt>
              <dd>{workflow.edges.length}</dd>
            </div>
            <div>
              <dt>Approvals</dt>
              <dd>{approvalNodeCount}</dd>
            </div>
          </dl>
          <div className="status-stack">
            <StatusRow label="Validation" value={validationState} tone={validationState} />
            <StatusRow label="Approval" value={approvalState} tone={approvalState} />
            <StatusRow label="Execution" value={executionState} tone={executionState} />
          </div>
        </aside>

        <section className="canvas-panel" aria-label="Workflow graph">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            defaultViewport={{ x: 16, y: 150, zoom: 0.86 }}
            minZoom={0.65}
            maxZoom={1.25}
          >
            <Background color="#cbd5e1" gap={18} />
            <MiniMap pannable zoomable />
            <Controls showInteractive={false} />
          </ReactFlow>
        </section>

        <aside className="panel inspector-panel" aria-label="Workflow inspector">
          <h2>Inspector</h2>
          <dl className="detail-list">
            <div>
              <dt>Workflow ID</dt>
              <dd>{workflow.id}</dd>
            </div>
            <div>
              <dt>Schema</dt>
              <dd>{workflow.schemaVersion}</dd>
            </div>
            <div>
              <dt>Revision</dt>
              <dd>{workflow.revision}</dd>
            </div>
            <div>
              <dt>Prompt</dt>
              <dd>{workflow.prompt}</dd>
            </div>
            <div>
              <dt>Frozen Approval</dt>
              <dd>{workflow.approval?.status ?? "draft"}</dd>
            </div>
          </dl>
        </aside>
      </section>
    </main>
  );
}

function StatusRow(props: {
  readonly label: string;
  readonly value: string;
  readonly tone: ValidationState | ApprovalState | ExecutionState;
}) {
  return (
    <div className="status-row">
      <span>{props.label}</span>
      <strong className={`status-pill status-${props.tone}`}>{props.value}</strong>
    </div>
  );
}
