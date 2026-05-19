import { useCallback, useMemo, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState
} from "@xyflow/react";
import type { Connection, Edge, EdgeChange, NodeChange } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  CheckCircle2,
  ChevronRight,
  Clock3,
  Database,
  FileStack,
  GitBranch,
  Grid2X2,
  History,
  Layers3,
  ListChecks,
  Paperclip,
  Play,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Trash2,
  Unplug,
  WandSparkles
} from "lucide-react";
import {
  createWorkflowEdge,
  createWorkflowNode,
  gmailReceiptsToSheetsWorkflowFixture,
  stableWorkflowStringify,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type {
  JsonRecord,
  WorkflowAdapterOperationRef,
  WorkflowApprovedRevision,
  WorkflowNode,
  WorkflowNodeKind,
  WorkflowRunRecord,
  WorkflowSpec,
  WorkflowSpecDiff,
  WorkflowValidationIssue,
  WorkflowValidationResult
} from "@kelpclaw/workflow-spec";
import { openClawApi } from "./api-client.js";
import {
  firstInputPort,
  firstOutputPort,
  nextNodePosition,
  nodeKindLabel,
  workflowNodeTypes,
  workflowToEdges,
  workflowToNodes
} from "./workflow-elements.js";
import type { WorkflowFlowEdge, WorkflowFlowNode } from "./workflow-elements.js";
import "./styles.css";

const nodeKinds: readonly WorkflowNodeKind[] = [
  "trigger",
  "skill",
  "codegen",
  "transform",
  "approval",
  "delivery"
];

const defaultPrompt = "extract transaction details from Gmail receipts into Sheets";
const initialSelectedNode =
  gmailReceiptsToSheetsWorkflowFixture.nodes.find((node) => node.id === "read-gmail-receipts") ??
  gmailReceiptsToSheetsWorkflowFixture.nodes[0] ??
  null;

interface AdapterSkillPreset {
  readonly id: string;
  readonly label: string;
  readonly nodeKinds: readonly WorkflowNodeKind[];
  readonly adapterIds: readonly string[];
  readonly adapterOperations: readonly WorkflowAdapterOperationRef[];
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly config: JsonRecord;
}

const adapterSkillPresets: readonly AdapterSkillPreset[] = [
  {
    id: "skill.gmail.receipts.read",
    label: "Gmail receipts",
    nodeKinds: ["skill"],
    adapterIds: ["adapter.gmail"],
    adapterOperations: [
      {
        adapterId: "adapter.gmail",
        operation: "gmail.receipts.search",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "gmail.oauth": "secret:google.oauth.default" },
    config: { query: "from:(receipts OR orders) newer_than:30d" }
  },
  {
    id: "skill.sheets.rows.append",
    label: "Sheets append",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.sheets"],
    adapterOperations: [
      {
        adapterId: "adapter.sheets",
        operation: "sheets.rows.append",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "sheets.oauth": "secret:google.oauth.default" },
    config: { channel: "sheets", channels: ["sheets"], range: "Receipts!A:D" }
  },
  {
    id: "skill.email.results.deliver",
    label: "Email results",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.email"],
    adapterOperations: [
      {
        adapterId: "adapter.email",
        operation: "email.results.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "email.delivery": "secret:email.smtp.default" },
    config: { channel: "email", channels: ["email"], to: "owner@example.com" }
  },
  {
    id: "skill.alert.push.dispatch",
    label: "Push alerts",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.whatsapp", "adapter.telegram"],
    adapterOperations: [
      {
        adapterId: "adapter.whatsapp",
        operation: "whatsapp.alert.send",
        operationVersion: "1.0.0"
      },
      {
        adapterId: "adapter.telegram",
        operation: "telegram.alert.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: {
      "whatsapp.apiKey": "secret:whatsapp.cloud.default",
      "telegram.botToken": "secret:telegram.bot.default"
    },
    config: { channel: "email", channels: ["whatsapp", "telegram"], timeSensitive: true }
  }
];

const componentCategories = [
  { label: "Input & Output", icon: Unplug },
  { label: "Data Sources", icon: Database },
  { label: "Models & Agents", icon: Layers3 },
  { label: "LLM Operations", icon: WandSparkles },
  { label: "Files & Knowledge", icon: FileStack },
  { label: "Processing", icon: SlidersHorizontal },
  { label: "Flow Control", icon: GitBranch }
] as const;

const railItems = [
  { label: "Search", icon: Search },
  { label: "Components", icon: Grid2X2 },
  { label: "Attachments", icon: Paperclip },
  { label: "History", icon: History }
] as const;

export function App() {
  const [workflow, setWorkflow] = useState<WorkflowSpec>(gmailReceiptsToSheetsWorkflowFixture);
  const [prompt, setPrompt] = useState(defaultPrompt);
  const [validation, setValidation] = useState<WorkflowValidationResult>(
    validateWorkflowSpec(gmailReceiptsToSheetsWorkflowFixture)
  );
  const [approvedRevision, setApprovedRevision] = useState<WorkflowApprovedRevision | null>(null);
  const [approvalDiff, setApprovalDiff] = useState<WorkflowSpecDiff | null>(null);
  const [run, setRun] = useState<WorkflowRunRecord | null>(null);
  const [dirtyNodeIds, setDirtyNodeIds] = useState<ReadonlySet<string>>(new Set());
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>("read-gmail-receipts");
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [nodePrompt, setNodePrompt] = useState(initialSelectedNode?.description ?? "");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [apiError, setApiError] = useState<string | null>(null);
  const [promotionNotice, setPromotionNotice] = useState<string | null>(null);

  const validationIssues = validation.ok ? [] : validation.errors;
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowFlowNode>(
    workflowToNodes(workflow, validationIssues)
  );
  const [edges, setEdges, onEdgesChangeBase] = useEdgesState<WorkflowFlowEdge>(
    workflowToEdges(workflow, validationIssues)
  );

  const selectedNode = useMemo(
    () => workflow.nodes.find((node) => node.id === selectedNodeId) ?? workflow.nodes[0] ?? null,
    [selectedNodeId, workflow.nodes]
  );
  const selectedEdge = useMemo(
    () => workflow.edges.find((edge) => edge.id === selectedEdgeId) ?? null,
    [selectedEdgeId, workflow.edges]
  );
  const canApprove = validation.ok;
  const canRun = approvedRevision !== null;

  const loadWorkflow = useCallback(
    (
      nextWorkflow: WorkflowSpec,
      nextValidation: WorkflowValidationResult = validateWorkflowSpec(nextWorkflow)
    ) => {
      const issues = nextValidation.ok ? [] : nextValidation.errors;
      setWorkflow(nextWorkflow);
      setValidation(nextValidation);
      setNodes(workflowToNodes(nextWorkflow, issues));
      setEdges(workflowToEdges(nextWorkflow, issues));
    },
    [setEdges, setNodes]
  );

  const updateLocalWorkflow = useCallback(
    (nextWorkflow: WorkflowSpec) => {
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
      loadWorkflow(nextWorkflow);
    },
    [loadWorkflow]
  );

  async function executeApiAction(action: string, work: () => Promise<void>) {
    setBusyAction(action);
    setApiError(null);
    try {
      await work();
    } catch (error) {
      setApiError(error instanceof Error ? error.message : "OpenClaw request failed.");
    } finally {
      setBusyAction(null);
    }
  }

  function markDirty(nodeId: string) {
    setDirtyNodeIds((previous) => new Set([...previous, nodeId]));
  }

  function updateNode(nodeId: string, updater: (node: WorkflowNode) => WorkflowNode) {
    const nextWorkflow: WorkflowSpec = {
      ...workflow,
      approval: null,
      nodes: workflow.nodes.map((node) => (node.id === nodeId ? updater(node) : node))
    };
    markDirty(nodeId);
    updateLocalWorkflow(nextWorkflow);
  }

  function onNodesChange(changes: NodeChange<WorkflowFlowNode>[]) {
    onNodesChangeBase(changes);
  }

  function onEdgesChange(changes: EdgeChange<WorkflowFlowEdge>[]) {
    onEdgesChangeBase(changes);
  }

  function onNodeDragStop(_: unknown, node: WorkflowFlowNode) {
    updateNode(node.id, (workflowNode) => ({
      ...workflowNode,
      config: {
        ...workflowNode.config,
        canvas: {
          x: Math.round(node.position.x),
          y: Math.round(node.position.y)
        }
      }
    }));
  }

  function onConnect(connection: Connection) {
    if (!connection.source || !connection.target) {
      return;
    }

    const sourceNode = workflow.nodes.find((node) => node.id === connection.source);
    const targetNode = workflow.nodes.find((node) => node.id === connection.target);
    const sourcePort =
      connection.sourceHandle ?? (sourceNode ? firstOutputPort(sourceNode) : undefined);
    const targetPort =
      connection.targetHandle ?? (targetNode ? firstInputPort(targetNode) : undefined);
    if (!sourceNode || !targetNode || !sourcePort || !targetPort) {
      return;
    }

    const edge = createWorkflowEdge({
      sourceNodeId: sourceNode.id,
      sourcePort,
      targetNodeId: targetNode.id,
      targetPort,
      id: uniqueEdgeId(sourceNode.id, targetNode.id, workflow.edges)
    });
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      edges: [...workflow.edges, edge]
    });
  }

  function addNode(kind: WorkflowNodeKind) {
    const id = uniqueNodeId(kind, workflow.nodes);
    const position = nextNodePosition(nodes);
    const node = createWorkflowNode({
      id,
      kind,
      config: {
        canvas: position
      }
    });
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: [...workflow.nodes, node]
    });
    setSelectedNodeId(id);
    setSelectedEdgeId(null);
    setNodePrompt(node.description);
    setJsonError(null);
    markDirty(id);
  }

  function deleteSelection() {
    if (selectedNodeId) {
      updateLocalWorkflow({
        ...workflow,
        approval: null,
        nodes: workflow.nodes.filter((node) => node.id !== selectedNodeId),
        edges: workflow.edges.filter(
          (edge) => edge.source.nodeId !== selectedNodeId && edge.target.nodeId !== selectedNodeId
        )
      });
      setSelectedNodeId(null);
      return;
    }

    if (selectedEdgeId) {
      updateLocalWorkflow({
        ...workflow,
        approval: null,
        edges: workflow.edges.filter((edge) => edge.id !== selectedEdgeId)
      });
      setSelectedEdgeId(null);
    }
  }

  function onNodesDelete(deletedNodes: WorkflowFlowNode[]) {
    const deletedIds = new Set(deletedNodes.map((node) => node.id));
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: workflow.nodes.filter((node) => !deletedIds.has(node.id)),
      edges: workflow.edges.filter(
        (edge) => !deletedIds.has(edge.source.nodeId) && !deletedIds.has(edge.target.nodeId)
      )
    });
  }

  function onEdgesDelete(deletedEdges: Edge[]) {
    const deletedIds = new Set(deletedEdges.map((edge) => edge.id));
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      edges: workflow.edges.filter((edge) => !deletedIds.has(edge.id))
    });
  }

  function selectIssue(issue: WorkflowValidationIssue) {
    const [collection, index] = issue.path;
    if (collection === "nodes" && typeof index === "number") {
      setSelectedNodeId(workflow.nodes[index]?.id ?? null);
      setSelectedEdgeId(null);
    } else if (collection === "edges" && typeof index === "number") {
      setSelectedEdgeId(workflow.edges[index]?.id ?? null);
      setSelectedNodeId(null);
    }
  }

  function updateJsonField(nodeId: string, field: "config" | "inputs" | "outputs", value: string) {
    const parsed = parseJsonRecord(value);
    if (!parsed.ok) {
      setJsonError(parsed.error);
      return;
    }

    setJsonError(null);
    updateNode(nodeId, (node) => ({
      ...node,
      [field]: parsed.value
    }));
  }

  function planDraft() {
    void executeApiAction("plan", async () => {
      const response = await openClawApi.plan({
        prompt,
        currentWorkflow: workflow,
        preserveNodeIds: [...dirtyNodeIds]
      });
      loadWorkflow(response.workflow, response.validation);
      const nextSelectedNode =
        response.workflow.nodes.find((node) => node.kind !== "trigger") ??
        response.workflow.nodes[0] ??
        null;
      setSelectedNodeId(nextSelectedNode?.id ?? null);
      setSelectedEdgeId(null);
      setDirtyNodeIds(new Set());
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
      setNodePrompt(nextSelectedNode?.description ?? "");
    });
  }

  function validateDraft() {
    void executeApiAction("validate", async () => {
      const response = await openClawApi.validate(workflow.id, { workflow });
      setValidation(response.validation);
      if (response.workflow) {
        loadWorkflow(response.workflow, response.validation);
      }
    });
  }

  function repromptNode() {
    if (!selectedNode) {
      return;
    }

    void executeApiAction("reprompt", async () => {
      const response = await openClawApi.repromptNode(workflow.id, {
        nodeId: selectedNode.id,
        prompt: nodePrompt,
        currentWorkflow: workflow
      });
      loadWorkflow(response.workflow, response.validation);
      setApprovalDiff(response.diff);
      markDirty(selectedNode.id);
      setPromotionNotice(null);
    });
  }

  function reviewCodegenNode() {
    if (!selectedNode || selectedNode.kind !== "codegen") {
      return;
    }

    void executeApiAction("review-codegen", async () => {
      const response = await openClawApi.reviewCodegen(workflow.id, selectedNode.id, {
        status: "approved",
        reviewedBy: "owner@example.com"
      });
      loadWorkflow(response.workflow, response.validation);
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
    });
  }

  function promoteCodegenNode() {
    if (!selectedNode || selectedNode.kind !== "codegen") {
      return;
    }

    void executeApiAction("promote-codegen", async () => {
      const response = await openClawApi.promoteCodegen(workflow.id, selectedNode.id);
      setPromotionNotice(`Promoted ${response.skill.name}`);
    });
  }

  function approveWorkflow() {
    void executeApiAction("approve", async () => {
      const response = await openClawApi.approve(workflow.id, {
        workflow,
        approvedBy: "owner@example.com"
      });
      setApprovedRevision(response.approvedRevision);
      setApprovalDiff(response.diff);
      loadWorkflow(response.workflow, validateWorkflowSpec(response.workflow));
    });
  }

  function runWorkflow() {
    if (!approvedRevision) {
      return;
    }

    void executeApiAction("run", async () => {
      const response = await openClawApi.startRun(workflow.id, {
        approvedRevisionId: approvedRevision.id
      });
      const fetched = await openClawApi.fetchRun(workflow.id, response.run.id);
      setRun(fetched.run);
    });
  }

  function resetWorkflow() {
    setPrompt(defaultPrompt);
    setDirtyNodeIds(new Set());
    setSelectedNodeId("read-gmail-receipts");
    setSelectedEdgeId(null);
    setNodePrompt(initialSelectedNode?.description ?? "");
    setJsonError(null);
    setApprovedRevision(null);
    setApprovalDiff(null);
    setRun(null);
    setPromotionNotice(null);
    loadWorkflow(
      gmailReceiptsToSheetsWorkflowFixture,
      validateWorkflowSpec(gmailReceiptsToSheetsWorkflowFixture)
    );
  }

  return (
    <main className="app-shell">
      <section className="workspace">
        <aside className="nav-rail" aria-label="Workspace navigation">
          {railItems.map((item, index) => {
            const Icon = item.icon;
            return (
              <button
                key={item.label}
                className={index === 1 ? "rail-button rail-button-active" : "rail-button"}
                type="button"
                title={item.label}
              >
                <Icon size={19} />
              </button>
            );
          })}
        </aside>

        <aside className="panel planner-panel" aria-label="Workflow planner">
          <div className="sidebar-search">
            <Search size={18} />
            <input aria-label="Search components" placeholder="Search" />
            <kbd>/</kbd>
          </div>

          <section className="component-browser" aria-label="Component categories">
            <div className="component-heading">
              <h2>Components</h2>
              <SlidersHorizontal size={16} />
            </div>
            <div className="component-list">
              {componentCategories.map((category, index) => {
                const Icon = category.icon;
                return (
                  <button
                    key={category.label}
                    className={index === 0 ? "component-row component-row-active" : "component-row"}
                    type="button"
                  >
                    <Icon size={18} />
                    <span>{category.label}</span>
                    <ChevronRight size={16} />
                  </button>
                );
              })}
            </div>
            <button className="discover-button" type="button">
              <Grid2X2 size={18} />
              Discover more components
            </button>
          </section>

          <form
            className="prompt-form"
            onSubmit={(event) => {
              event.preventDefault();
              planDraft();
            }}
          >
            <label htmlFor="workflow-prompt">Workflow Prompt</label>
            <textarea
              id="workflow-prompt"
              value={prompt}
              onChange={(event) => setPrompt(event.target.value)}
              rows={4}
            />
            <button type="submit" disabled={busyAction !== null || prompt.trim().length === 0}>
              <WandSparkles size={18} />
              Plan
            </button>
          </form>

          <section aria-label="Workflow summary">
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
                <dt>Revision</dt>
                <dd>{workflow.revision}</dd>
              </div>
            </dl>
          </section>

          <section aria-label="Validation panel" className="validation-panel">
            <div className="panel-heading">
              <ListChecks size={18} />
              <h2>Validation</h2>
            </div>
            <StatusRow
              label="Graph"
              value={validation.ok ? "valid" : "blocked"}
              tone={validation.ok ? "valid" : "blocked"}
            />
            {validationIssues.length > 0 ? (
              <div className="issue-list">
                {validationIssues.map((issue) => (
                  <button
                    key={`${issue.code}-${issue.path.join(".")}`}
                    className="issue-button"
                    onClick={() => selectIssue(issue)}
                    type="button"
                  >
                    <strong>{issue.code}</strong>
                    <span>{issue.message}</span>
                  </button>
                ))}
              </div>
            ) : null}
          </section>

          {apiError ? <p className="error-text">{apiError}</p> : null}
        </aside>

        <section className="canvas-panel" aria-label="Workflow graph">
          <header className="topbar">
            <div>
              <p className="eyebrow">KelpClaw</p>
              <h1>OpenClaw</h1>
            </div>
            <div className="topbar-actions" aria-label="Workflow actions">
              <button
                title="Validate workflow"
                onClick={validateDraft}
                disabled={busyAction !== null}
              >
                <ShieldCheck size={18} />
                Validate
              </button>
              <button
                title="Approve workflow"
                onClick={approveWorkflow}
                disabled={!canApprove || busyAction !== null}
              >
                <CheckCircle2 size={18} />
                Approve
              </button>
              <button
                title="Run workflow"
                onClick={runWorkflow}
                disabled={!canRun || busyAction !== null}
              >
                <Play size={18} />
                Run
              </button>
              <button className="icon-button" title="Reset workflow" onClick={resetWorkflow}>
                <RefreshCw size={18} />
              </button>
            </div>
          </header>
          <div className="canvas-toolbar" aria-label="Canvas controls">
            <div className="node-kind-actions">
              {nodeKinds.map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => addNode(kind)}
                  title={`Add ${kind} node`}
                >
                  <Plus size={16} />
                  {nodeKindLabel(kind)}
                </button>
              ))}
            </div>
            <button
              className="icon-button"
              type="button"
              onClick={deleteSelection}
              title="Delete selected"
              disabled={!selectedNodeId && !selectedEdgeId}
            >
              <Trash2 size={18} />
            </button>
          </div>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={workflowNodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeDragStop={onNodeDragStop}
            onConnect={onConnect}
            onNodesDelete={onNodesDelete}
            onEdgesDelete={onEdgesDelete}
            onSelectionChange={({ nodes: selectedNodes, edges: selectedEdges }) => {
              if (selectedNodes[0]) {
                setSelectedNodeId(selectedNodes[0].id);
                setSelectedEdgeId(null);
                setNodePrompt(
                  workflow.nodes.find((node) => node.id === selectedNodes[0]?.id)?.description ?? ""
                );
                setJsonError(null);
              } else if (selectedEdges[0]) {
                setSelectedNodeId(null);
                setSelectedEdgeId(selectedEdges[0].id);
                setNodePrompt("");
                setJsonError(null);
              }
            }}
            fitView
            minZoom={0.5}
            maxZoom={1.35}
          >
            <Background color="#272a32" gap={18} size={1.15} />
            <MiniMap pannable zoomable />
            <Controls showInteractive={false} />
          </ReactFlow>
          <div className="canvas-footer" aria-label="Canvas status">
            <span className="canvas-wave">~</span>
            <span>63%</span>
            <Clock3 size={18} />
            <span>{workflow.nodes.length} nodes</span>
          </div>
        </section>

        <aside className="panel inspector-panel" aria-label="Workflow inspector">
          <Inspector
            workflow={workflow}
            selectedNode={selectedNode}
            selectedEdgeId={selectedEdge?.id ?? null}
            nodePrompt={nodePrompt}
            jsonError={jsonError}
            approvalDiff={approvalDiff}
            approvedRevision={approvedRevision}
            run={run}
            busyAction={busyAction}
            onNodePromptChange={setNodePrompt}
            onReprompt={repromptNode}
            onReviewCodegen={reviewCodegenNode}
            onPromoteCodegen={promoteCodegenNode}
            onUpdateNode={updateNode}
            onUpdateJsonField={updateJsonField}
            promotionNotice={promotionNotice}
          />
        </aside>
      </section>
    </main>
  );
}

function Inspector(props: {
  readonly workflow: WorkflowSpec;
  readonly selectedNode: WorkflowNode | null;
  readonly selectedEdgeId: string | null;
  readonly nodePrompt: string;
  readonly jsonError: string | null;
  readonly approvalDiff: WorkflowSpecDiff | null;
  readonly approvedRevision: WorkflowApprovedRevision | null;
  readonly run: WorkflowRunRecord | null;
  readonly busyAction: string | null;
  readonly promotionNotice: string | null;
  readonly onNodePromptChange: (value: string) => void;
  readonly onReprompt: () => void;
  readonly onReviewCodegen: () => void;
  readonly onPromoteCodegen: () => void;
  readonly onUpdateNode: (nodeId: string, updater: (node: WorkflowNode) => WorkflowNode) => void;
  readonly onUpdateJsonField: (
    nodeId: string,
    field: "config" | "inputs" | "outputs",
    value: string
  ) => void;
}) {
  const node = props.selectedNode;

  return (
    <>
      <h2>Inspector</h2>
      {node ? (
        <div className="inspector-stack">
          <label>
            Label
            <input
              value={node.label}
              onChange={(event) =>
                props.onUpdateNode(node.id, (current) => ({
                  ...current,
                  label: event.target.value
                }))
              }
            />
          </label>
          <label>
            Description
            <textarea
              value={node.description}
              rows={3}
              onChange={(event) =>
                props.onUpdateNode(node.id, (current) => ({
                  ...current,
                  description: event.target.value
                }))
              }
            />
          </label>
          {node.kind === "skill" || node.kind === "delivery" ? (
            <label>
              Adapter-backed skill
              <select
                value={node.skillId ?? ""}
                onChange={(event) =>
                  props.onUpdateNode(node.id, (current) =>
                    applyAdapterSkillPreset(current, event.target.value)
                  )
                }
              >
                <option value="">Unassigned</option>
                {adapterSkillPresets
                  .filter((preset) => preset.nodeKinds.includes(node.kind))
                  .map((preset) => (
                    <option key={preset.id} value={preset.id}>
                      {preset.label}
                    </option>
                  ))}
              </select>
            </label>
          ) : null}
          <label>
            Config
            <textarea
              key={`config-${node.id}-${props.workflow.revision}`}
              defaultValue={formatJson(node.config)}
              rows={6}
              onBlur={(event) => props.onUpdateJsonField(node.id, "config", event.target.value)}
            />
          </label>
          <div className="inline-grid">
            <label>
              Timeout
              <input
                type="number"
                min={1}
                value={node.runtime.timeoutSeconds}
                onChange={(event) =>
                  props.onUpdateNode(node.id, (current) => ({
                    ...current,
                    runtime: {
                      ...current.runtime,
                      timeoutSeconds: Number(event.target.value)
                    }
                  }))
                }
              />
            </label>
            <label>
              Retries
              <input
                type="number"
                min={0}
                value={node.runtime.retry.maxAttempts}
                onChange={(event) =>
                  props.onUpdateNode(node.id, (current) => ({
                    ...current,
                    runtime: {
                      ...current.runtime,
                      retry: {
                        ...current.runtime.retry,
                        maxAttempts: Number(event.target.value)
                      }
                    }
                  }))
                }
              />
            </label>
          </div>
          <div className="inline-grid">
            <label>
              Inputs
              <textarea
                key={`inputs-${node.id}-${props.workflow.revision}`}
                defaultValue={formatJson(node.inputs)}
                rows={4}
                onBlur={(event) => props.onUpdateJsonField(node.id, "inputs", event.target.value)}
              />
            </label>
            <label>
              Outputs
              <textarea
                key={`outputs-${node.id}-${props.workflow.revision}`}
                defaultValue={formatJson(node.outputs)}
                rows={4}
                onBlur={(event) => props.onUpdateJsonField(node.id, "outputs", event.target.value)}
              />
            </label>
          </div>
          {node.kind === "delivery" ? (
            <div className="delivery-controls">
              <label>
                Primary channel
                <select
                  value={String(node.config.channel ?? "sheets")}
                  onChange={(event) =>
                    props.onUpdateNode(node.id, (current) =>
                      updatePrimaryDeliveryChannel(current, event.target.value)
                    )
                  }
                >
                  <option value="sheets">Sheets</option>
                  <option value="email">Email</option>
                </select>
              </label>
              <div className="channel-checkboxes" aria-label="Secondary push channels">
                <label>
                  <input
                    type="checkbox"
                    checked={deliveryChannels(node).has("whatsapp")}
                    onChange={(event) =>
                      props.onUpdateNode(node.id, (current) =>
                        toggleSecondaryDeliveryChannel(current, "whatsapp", event.target.checked)
                      )
                    }
                  />
                  WhatsApp
                </label>
                <label>
                  <input
                    type="checkbox"
                    checked={deliveryChannels(node).has("telegram")}
                    onChange={(event) =>
                      props.onUpdateNode(node.id, (current) =>
                        toggleSecondaryDeliveryChannel(current, "telegram", event.target.checked)
                      )
                    }
                  />
                  Telegram
                </label>
              </div>
              <label>
                Adapter
                <input
                  value={(node.adapterIds ?? (node.adapterId ? [node.adapterId] : [])).join(", ")}
                  onChange={(event) =>
                    props.onUpdateNode(node.id, (current) =>
                      updateAdapterIds(current, event.target.value)
                    )
                  }
                />
              </label>
            </div>
          ) : null}
          <label>
            Node Prompt
            <textarea
              value={props.nodePrompt}
              rows={3}
              onChange={(event) => props.onNodePromptChange(event.target.value)}
            />
          </label>
          <button type="button" onClick={props.onReprompt} disabled={props.busyAction !== null}>
            <WandSparkles size={18} />
            Reprompt Node
          </button>
          {node.kind === "codegen" ? (
            <section className="codegen-panel" aria-label="Generated code controls">
              <StatusRow
                label="Review"
                value={node.codegen?.review.status ?? "missing"}
                tone={node.codegen?.review.status ?? "blocked"}
              />
              <StatusRow
                label="Replay"
                value={node.codegen?.replay.mode ?? "missing"}
                tone="pending"
              />
              <button
                type="button"
                onClick={props.onReviewCodegen}
                disabled={props.busyAction !== null || node.codegen?.review.status === "approved"}
              >
                <CheckCircle2 size={18} />
                Review Generated Code
              </button>
              <button
                type="button"
                onClick={props.onPromoteCodegen}
                disabled={props.busyAction !== null || node.codegen?.review.status !== "approved"}
              >
                <WandSparkles size={18} />
                Promote Skill
              </button>
              {props.promotionNotice ? (
                <p className="success-text">{props.promotionNotice}</p>
              ) : null}
            </section>
          ) : null}
          {props.jsonError ? <p className="error-text">{props.jsonError}</p> : null}
        </div>
      ) : (
        <dl className="detail-list">
          <div>
            <dt>Workflow ID</dt>
            <dd>{props.workflow.id}</dd>
          </div>
          <div>
            <dt>Selected Edge</dt>
            <dd>{props.selectedEdgeId ?? "none"}</dd>
          </div>
          <div>
            <dt>Frozen Approval</dt>
            <dd>{props.workflow.approval?.status ?? "draft"}</dd>
          </div>
        </dl>
      )}

      <ApprovalPanel diff={props.approvalDiff} approvedRevision={props.approvedRevision} />
      <RunPanel run={props.run} />
    </>
  );
}

function ApprovalPanel(props: {
  readonly diff: WorkflowSpecDiff | null;
  readonly approvedRevision: WorkflowApprovedRevision | null;
}) {
  return (
    <section className="approval-panel" aria-label="Approval diff">
      <h2>Approval</h2>
      <StatusRow
        label="Revision"
        value={props.approvedRevision ? `r${props.approvedRevision.revision}` : "draft"}
        tone={props.approvedRevision ? "approved" : "pending"}
      />
      {props.diff ? (
        <>
          <ul className="diff-summary">
            {props.diff.summary.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          <pre className="diff-view" data-testid="approval-diff">
            {props.diff.lines
              .filter((line) => line.kind !== "same")
              .slice(0, 80)
              .map((line) => `${line.kind === "added" ? "+" : "-"} ${line.text}`)
              .join("\n")}
          </pre>
        </>
      ) : null}
    </section>
  );
}

function RunPanel(props: { readonly run: WorkflowRunRecord | null }) {
  return (
    <section className="run-panel" aria-label="Run status">
      <h2>Run</h2>
      <StatusRow
        label="Status"
        value={props.run?.status ?? "idle"}
        tone={props.run?.status ?? "idle"}
      />
      {props.run ? (
        <>
          <ul className="event-list">
            {props.run.events.map((event) => (
              <li key={event.id}>
                <strong>{event.level}</strong>
                <span>{event.message}</span>
              </li>
            ))}
          </ul>
          <pre className="result-view">{formatJson(props.run.result ?? {})}</pre>
        </>
      ) : null}
    </section>
  );
}

function StatusRow(props: {
  readonly label: string;
  readonly value: string;
  readonly tone: string;
}) {
  return (
    <div className="status-row">
      <span>{props.label}</span>
      <strong className={`status-pill status-${props.tone}`}>{props.value}</strong>
    </div>
  );
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

function parseJsonRecord(
  value: string
):
  | { readonly ok: true; readonly value: JsonRecord }
  | { readonly ok: false; readonly error: string } {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return { ok: false, error: "JSON value must be an object." };
    }

    return { ok: true, value: parsed as JsonRecord };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "Invalid JSON."
    };
  }
}

function applyAdapterSkillPreset(node: WorkflowNode, skillId: string): WorkflowNode {
  const preset = adapterSkillPresets.find((candidate) => candidate.id === skillId);
  if (!preset) {
    return withoutAdapterSkill(node);
  }

  return {
    ...node,
    skillId: preset.id,
    adapterId: preset.adapterIds[0],
    adapterIds: preset.adapterIds,
    adapterOperations: preset.adapterOperations,
    secretRefs: preset.secretRefs,
    config: {
      ...node.config,
      ...preset.config
    }
  };
}

function withoutAdapterSkill(node: WorkflowNode): WorkflowNode {
  return {
    id: node.id,
    kind: node.kind,
    label: node.label,
    description: node.description,
    inputs: node.inputs,
    outputs: node.outputs,
    config: node.config,
    runtime: node.runtime,
    determinism: node.determinism,
    ...(node.codegen ? { codegen: node.codegen } : {})
  };
}

function updatePrimaryDeliveryChannel(node: WorkflowNode, channel: string): WorkflowNode {
  const channels = deliveryChannels(node);
  const nextChannels = new Set(
    [...channels].filter((candidate) => candidate !== "email" && candidate !== "sheets")
  );
  nextChannels.add(channel);

  return withDeliveryAdapters({
    ...node,
    config: {
      ...node.config,
      channel,
      channels: [...nextChannels].sort()
    }
  });
}

function toggleSecondaryDeliveryChannel(
  node: WorkflowNode,
  channel: "whatsapp" | "telegram",
  enabled: boolean
): WorkflowNode {
  const channels = new Set(deliveryChannels(node));
  if (enabled) {
    channels.add(channel);
  } else {
    channels.delete(channel);
  }

  return withDeliveryAdapters({
    ...node,
    config: {
      ...node.config,
      channels: [...channels].sort()
    }
  });
}

function updateAdapterIds(node: WorkflowNode, value: string): WorkflowNode {
  const adapterIds = value
    .split(",")
    .map((adapterId) => adapterId.trim())
    .filter(Boolean);

  return {
    ...node,
    ...(adapterIds[0] ? { adapterId: adapterIds[0] } : {}),
    adapterIds,
    adapterOperations: node.adapterOperations?.filter((operation) =>
      adapterIds.includes(operation.adapterId)
    )
  };
}

function withDeliveryAdapters(node: WorkflowNode): WorkflowNode {
  const declarations = adapterDeclarationsForChannels(deliveryChannels(node));

  return {
    ...node,
    adapterId: declarations.adapterIds[0],
    adapterIds: declarations.adapterIds,
    adapterOperations: declarations.adapterOperations,
    secretRefs: {
      ...(node.secretRefs ?? {}),
      ...declarations.secretRefs
    }
  };
}

function adapterDeclarationsForChannels(channels: ReadonlySet<string>): {
  readonly adapterIds: readonly string[];
  readonly adapterOperations: readonly WorkflowAdapterOperationRef[];
  readonly secretRefs: Readonly<Record<string, string>>;
} {
  const adapterIds: string[] = [];
  const adapterOperations: WorkflowAdapterOperationRef[] = [];
  const secretRefs: Record<string, string> = {};

  for (const channel of [...channels].sort()) {
    const declaration = adapterDeclarationForChannel(channel);
    if (!declaration) {
      continue;
    }
    adapterIds.push(declaration.adapterId);
    adapterOperations.push(declaration.operation);
    Object.assign(secretRefs, declaration.secretRefs);
  }

  return {
    adapterIds,
    adapterOperations,
    secretRefs
  };
}

function adapterDeclarationForChannel(channel: string):
  | {
      readonly adapterId: string;
      readonly operation: WorkflowAdapterOperationRef;
      readonly secretRefs: Readonly<Record<string, string>>;
    }
  | undefined {
  switch (channel) {
    case "email":
      return {
        adapterId: "adapter.email",
        operation: {
          adapterId: "adapter.email",
          operation: "email.results.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "email.delivery": "secret:email.smtp.default" }
      };
    case "sheets":
      return {
        adapterId: "adapter.sheets",
        operation: {
          adapterId: "adapter.sheets",
          operation: "sheets.rows.append",
          operationVersion: "1.0.0"
        },
        secretRefs: { "sheets.oauth": "secret:google.oauth.default" }
      };
    case "whatsapp":
      return {
        adapterId: "adapter.whatsapp",
        operation: {
          adapterId: "adapter.whatsapp",
          operation: "whatsapp.alert.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "whatsapp.apiKey": "secret:whatsapp.cloud.default" }
      };
    case "telegram":
      return {
        adapterId: "adapter.telegram",
        operation: {
          adapterId: "adapter.telegram",
          operation: "telegram.alert.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "telegram.botToken": "secret:telegram.bot.default" }
      };
    default:
      return undefined;
  }
}

function deliveryChannels(node: WorkflowNode): ReadonlySet<string> {
  const channels = new Set<string>();
  const configuredChannels = node.config.channels;
  if (Array.isArray(configuredChannels)) {
    for (const channel of configuredChannels) {
      if (typeof channel === "string") {
        channels.add(channel);
      }
    }
  }
  if (typeof node.config.channel === "string") {
    channels.add(node.config.channel);
  }
  if (channels.size === 0 && node.kind === "delivery") {
    channels.add("email");
  }

  return channels;
}

function uniqueNodeId(kind: WorkflowNodeKind, nodes: readonly WorkflowNode[]): string {
  const prefix = `${kind}-node`;
  const existing = new Set(nodes.map((node) => node.id));
  let index = nodes.length + 1;
  let id = `${prefix}-${index}`;
  while (existing.has(id)) {
    index += 1;
    id = `${prefix}-${index}`;
  }

  return id;
}

function uniqueEdgeId(
  sourceId: string,
  targetId: string,
  edges: readonly { readonly id: string }[]
): string {
  const prefix = `edge.${sourceId}.${targetId}`;
  const existing = new Set(edges.map((edge) => edge.id));
  let index = 1;
  let id = `${prefix}.${index}`;
  while (existing.has(id)) {
    index += 1;
    id = `${prefix}.${index}`;
  }

  return id;
}

export function workflowJsonForDiff(workflow: WorkflowSpec): string {
  return stableWorkflowStringify(workflow);
}
