import type { Edge, Node } from "@xyflow/react";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";

const positions: Record<string, { readonly x: number; readonly y: number }> = {
  "collect-brief": { x: 20, y: 190 },
  "draft-copy": { x: 180, y: 130 },
  "owner-approval": { x: 340, y: 190 },
  "send-email": { x: 500, y: 130 }
};

export function workflowToNodes(workflow: WorkflowSpec): Node[] {
  return workflow.nodes.map((node, index) => ({
    id: node.id,
    position: positions[node.id] ?? { x: 120 + index * 260, y: 160 },
    data: {
      label: `${node.label} · ${node.type}`
    },
    type: "default",
    className: `workflow-node workflow-node-${node.type}`
  }));
}

export function workflowToEdges(workflow: WorkflowSpec): Edge[] {
  return workflow.edges.map((edge, index) => ({
    id: edge.id ?? `${edge.source}-${edge.target}-${index}`,
    source: edge.source,
    target: edge.target,
    animated: true,
    className: "workflow-edge"
  }));
}
