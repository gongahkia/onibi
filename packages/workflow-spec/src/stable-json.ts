import type { JsonValue, WorkflowEdge, WorkflowNode, WorkflowSpec } from "./types.js";

export function normalizeWorkflowSpec(workflow: WorkflowSpec): WorkflowSpec {
  return {
    ...workflow,
    nodes: [...workflow.nodes].sort((left, right) =>
      nodeSortKey(left).localeCompare(nodeSortKey(right))
    ),
    edges: [...workflow.edges].sort((left, right) =>
      edgeSortKey(left).localeCompare(edgeSortKey(right))
    ),
    approval: workflow.approval
      ? {
          ...workflow.approval,
          nodeOrder: [...workflow.approval.nodeOrder]
        }
      : null
  };
}

export function stableWorkflowStringify(workflow: WorkflowSpec): string {
  return stableJsonStringify(normalizeWorkflowSpec(workflow));
}

export function stableJsonStringify(value: JsonValue | WorkflowSpec): string {
  return JSON.stringify(sortJsonValue(value as JsonValue), null, 2);
}

function sortJsonValue(value: JsonValue): JsonValue {
  if (Array.isArray(value)) {
    return value.map((item) => sortJsonValue(item));
  }

  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, entryValue]) => entryValue !== undefined)
        .sort(([leftKey], [rightKey]) => leftKey.localeCompare(rightKey))
        .map(([key, entryValue]) => [key, sortJsonValue(entryValue)])
    );
  }

  return value;
}

function nodeSortKey(node: WorkflowNode): string {
  return `${node.kind}:${node.id}`;
}

function edgeSortKey(edge: WorkflowEdge): string {
  return `${edge.source.nodeId}:${edge.source.port}->${edge.target.nodeId}:${edge.target.port}:${edge.id}`;
}
