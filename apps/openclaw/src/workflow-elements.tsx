import { Handle, Position } from "@xyflow/react";
import type { Edge, Node, NodeProps } from "@xyflow/react";
import { PanelRightOpen, Plus, Trash2, WandSparkles } from "lucide-react";
import type { SyntheticEvent } from "react";
import type {
  WorkflowEdge,
  WorkflowNode,
  WorkflowNodeKind,
  WorkflowSpec,
  WorkflowValidationIssue
} from "@kelpclaw/workflow-spec";

export interface WorkflowNodeData extends Record<string, unknown> {
  readonly workflowNode: WorkflowNode;
  readonly issueCount: number;
  readonly onInlineEdit?: (
    nodeId: string,
    patch: Pick<WorkflowNode, "label" | "description">
  ) => void;
  readonly onSelectNode?: (nodeId: string) => void;
  readonly onOpenDetails?: (nodeId: string) => void;
  readonly onDeleteNode?: (nodeId: string) => void;
  readonly onRepromptNode?: (nodeId: string) => void;
  readonly onAddNextNode?: (nodeId: string, outputPort: string | undefined) => void;
}

export type WorkflowFlowNode = Node<WorkflowNodeData, "workflowNode">;
export type WorkflowFlowEdge = Edge<{ readonly workflowEdge: WorkflowEdge }>;

const fallbackPositions: Record<string, { readonly x: number; readonly y: number }> = {
  "manual-trigger": { x: 20, y: 190 },
  "read-gmail-receipts": { x: 230, y: 130 },
  "normalize-receipts": { x: 470, y: 190 },
  "append-sheet-rows": { x: 710, y: 130 },
  "daily-schedule": { x: 20, y: 190 },
  "scrape-status-page": { x: 260, y: 130 },
  "summarize-incidents": { x: 520, y: 190 },
  "email-trigger": { x: 20, y: 190 },
  "classify-urgency": { x: 230, y: 130 },
  "approve-alert": { x: 470, y: 190 },
  "send-alert": { x: 710, y: 130 }
};

export const workflowNodeTypes = {
  workflowNode: WorkflowNodeCard
} as const;

export function workflowToNodes(
  workflow: WorkflowSpec,
  issues: readonly WorkflowValidationIssue[] = []
): WorkflowFlowNode[] {
  return workflow.nodes.map((node, index) => {
    const canvas = readCanvasPosition(node);
    return {
      id: node.id,
      position: canvas ?? fallbackPositions[node.id] ?? { x: 120 + index * 260, y: 160 },
      data: {
        workflowNode: node,
        issueCount: countNodeIssues(node.id, issues, workflow)
      },
      type: "workflowNode",
      className: `workflow-node-shell workflow-node-${node.kind}`
    };
  });
}

export function workflowToEdges(
  workflow: WorkflowSpec,
  issues: readonly WorkflowValidationIssue[] = []
): WorkflowFlowEdge[] {
  return workflow.edges.map((edge, index) => ({
    id: edge.id,
    source: edge.source.nodeId,
    target: edge.target.nodeId,
    sourceHandle: edge.source.port,
    targetHandle: edge.target.port,
    label: `${edge.source.port} -> ${edge.target.port}`,
    type: "smoothstep",
    animated: false,
    className:
      countEdgeIssues(index, issues) > 0 ? "workflow-edge workflow-edge-invalid" : "workflow-edge",
    data: {
      workflowEdge: edge
    }
  }));
}

export function workflowWithNodePositions(
  workflow: WorkflowSpec,
  nodes: readonly Pick<WorkflowFlowNode, "id" | "position">[]
): WorkflowSpec {
  const positions = new Map(nodes.map((node) => [node.id, node.position]));

  return {
    ...workflow,
    nodes: workflow.nodes.map((node) => {
      const position = positions.get(node.id);
      if (!position) {
        return node;
      }

      return {
        ...node,
        config: {
          ...node.config,
          canvas: {
            x: Math.round(position.x),
            y: Math.round(position.y)
          }
        }
      };
    })
  };
}

export function firstOutputPort(node: WorkflowNode): string | undefined {
  return Object.keys(node.outputs)[0];
}

export function firstInputPort(node: WorkflowNode): string | undefined {
  return Object.keys(node.inputs)[0];
}

export function nextNodePosition(nodes: readonly WorkflowFlowNode[]): {
  readonly x: number;
  readonly y: number;
} {
  const rightmost = nodes.reduce((max, node) => Math.max(max, node.position.x), 0);
  return {
    x: rightmost + 230,
    y: 210 + (nodes.length % 3) * 72
  };
}

function WorkflowNodeCard(props: NodeProps<WorkflowFlowNode>) {
  const node = props.data.workflowNode;
  const inputPorts = Object.keys(node.inputs);
  const outputPorts = Object.keys(node.outputs);
  const selected = Boolean(props.selected);

  return (
    <div
      className={`workflow-card workflow-card-${node.kind}${selected ? " workflow-card-selected" : ""}`}
      onClick={(event) => {
        event.stopPropagation();
        props.data.onSelectNode?.(node.id);
      }}
      onDoubleClick={(event) => {
        event.stopPropagation();
        props.data.onOpenDetails?.(node.id);
      }}
    >
      {inputPorts.map((port, index) => (
        <Handle
          key={port}
          id={port}
          type="target"
          position={Position.Left}
          className="workflow-handle workflow-handle-input"
          style={{ top: `${portOffset(index, inputPorts.length)}%` }}
        />
      ))}
      <div className="workflow-card-header">
        <div className="node-title">
          <span className="node-glyph">{nodeGlyph(node.kind)}</span>
          <div className="node-title-copy">
            {selected ? (
              <input
                aria-label="Node label"
                className="node-inline-input nodrag"
                value={node.label}
                onChange={(event) =>
                  props.data.onInlineEdit?.(node.id, {
                    label: event.target.value,
                    description: node.description
                  })
                }
                onDoubleClick={stopNodeEvent}
                onPointerDown={stopNodeEvent}
              />
            ) : (
              <strong title={node.label}>{node.label}</strong>
            )}
            <span className="node-compact-meta">
              {node.kind} · {nodeStatusLabel(node)}
            </span>
          </div>
        </div>
        {props.data.issueCount > 0 ? (
          <span className="node-issues" aria-label={`${props.data.issueCount} validation issues`}>
            {props.data.issueCount}
          </span>
        ) : null}
      </div>
      {selected ? (
        <div className="node-inline-editor">
          <label className="node-inline-label">
            Description
            <textarea
              aria-label="Node description"
              className="node-inline-textarea nodrag"
              rows={3}
              value={node.description}
              onChange={(event) =>
                props.data.onInlineEdit?.(node.id, {
                  label: node.label,
                  description: event.target.value
                })
              }
              onDoubleClick={stopNodeEvent}
              onPointerDown={stopNodeEvent}
            />
          </label>
          <div className="node-port-chips" aria-label="Node ports and status">
            <span>{portCount(node.inputs)} in</span>
            <span>{portCount(node.outputs)} out</span>
            <span>{nodeStatusLabel(node)}</span>
          </div>
          <div className="node-selection-actions" aria-label="Selected node actions">
            <button
              className="icon-button"
              type="button"
              title="Open details"
              onClick={(event) => {
                event.stopPropagation();
                props.data.onOpenDetails?.(node.id);
              }}
              onPointerDown={stopNodeEvent}
            >
              <PanelRightOpen size={16} />
            </button>
            <button
              className="icon-button"
              type="button"
              title="Reprompt node"
              onClick={(event) => {
                event.stopPropagation();
                props.data.onRepromptNode?.(node.id);
              }}
              onPointerDown={stopNodeEvent}
            >
              <WandSparkles size={16} />
            </button>
            <button
              className="icon-button"
              type="button"
              title="Delete node"
              onClick={(event) => {
                event.stopPropagation();
                props.data.onDeleteNode?.(node.id);
              }}
              onPointerDown={stopNodeEvent}
            >
              <Trash2 size={16} />
            </button>
          </div>
          {outputPorts.length > 0 ? (
            <div className="node-next-actions" aria-label="Add connected node">
              {outputPorts.map((port) => (
                <button
                  key={port}
                  type="button"
                  title={`Add node after ${port}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    props.data.onAddNextNode?.(node.id, port);
                  }}
                  onPointerDown={stopNodeEvent}
                >
                  <Plus size={14} />
                  {port}
                </button>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
      {outputPorts.map((port, index) => (
        <Handle
          key={port}
          id={port}
          type="source"
          position={Position.Right}
          className="workflow-handle workflow-handle-output"
          style={{ top: `${portOffset(index, outputPorts.length)}%` }}
        />
      ))}
    </div>
  );
}

function stopNodeEvent(event: SyntheticEvent) {
  event.stopPropagation();
}

function nodeGlyph(kind: WorkflowNodeKind): string {
  switch (kind) {
    case "trigger":
      return ">";
    case "skill":
      return "#";
    case "codegen":
      return "{}";
    case "transform":
      return "<>";
    case "approval":
      return "ok";
    case "delivery":
      return "->";
  }
}

function nodeStatusLabel(node: WorkflowNode): string {
  if (node.kind === "codegen") {
    return node.codegen?.review.status === "approved"
      ? "reviewed"
      : (node.codegen?.review.status ?? "draft");
  }
  if (node.adapterOperations && node.adapterOperations.length > 0) {
    return "adapter";
  }

  return "ready";
}

function portOffset(index: number, total: number): number {
  if (total <= 1) {
    return 50;
  }

  return 25 + (index * 50) / (total - 1);
}

function portCount(ports: Readonly<Record<string, unknown>>): number {
  return Object.keys(ports).length;
}

function readCanvasPosition(
  node: WorkflowNode
): { readonly x: number; readonly y: number } | undefined {
  const canvas = node.config.canvas;
  if (
    typeof canvas === "object" &&
    canvas !== null &&
    !Array.isArray(canvas) &&
    typeof canvas.x === "number" &&
    typeof canvas.y === "number"
  ) {
    return { x: canvas.x, y: canvas.y };
  }

  return undefined;
}

function countNodeIssues(
  nodeId: string,
  issues: readonly WorkflowValidationIssue[],
  workflow: WorkflowSpec
): number {
  return issues.filter((issue) => {
    const [collection, index] = issue.path;
    if (collection !== "nodes" || typeof index !== "number") {
      return false;
    }

    return workflow.nodes[index]?.id === nodeId;
  }).length;
}

function countEdgeIssues(index: number, issues: readonly WorkflowValidationIssue[]): number {
  return issues.filter((issue) => issue.path[0] === "edges" && issue.path[1] === index).length;
}

export function nodeKindLabel(kind: WorkflowNodeKind): string {
  return kind
    .split("-")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}
