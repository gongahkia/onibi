import type { Edge, Node } from "@xyflow/react";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";

const positions: Record<string, { readonly x: number; readonly y: number }> = {
  "manual-trigger": { x: 20, y: 190 },
  "read-gmail-receipts": { x: 190, y: 130 },
  "normalize-receipts": { x: 360, y: 190 },
  "append-sheet-rows": { x: 530, y: 130 }
};

export function workflowToNodes(workflow: WorkflowSpec): Node[] {
  return workflow.nodes.map((node, index) => ({
    id: node.id,
    position: positions[node.id] ?? { x: 120 + index * 260, y: 160 },
    data: {
      label: `${node.label} · ${node.kind}`
    },
    type: "default",
    className: `workflow-node workflow-node-${node.kind}`
  }));
}

export function workflowToEdges(workflow: WorkflowSpec): Edge[] {
  return workflow.edges.map((edge) => ({
    id: edge.id,
    source: edge.source.nodeId,
    target: edge.target.nodeId,
    label: `${edge.source.port} → ${edge.target.port}`,
    animated: true,
    className: "workflow-edge"
  }));
}
