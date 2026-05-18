import type { JsonValue, WorkflowSpec } from "./types.js";

export function normalizeWorkflowSpec(workflow: WorkflowSpec): WorkflowSpec {
  return {
    ...workflow,
    nodes: [...workflow.nodes].sort((left, right) => left.id.localeCompare(right.id)),
    edges: [...workflow.edges].sort((left, right) =>
      edgeSortKey(left).localeCompare(edgeSortKey(right))
    ),
    approvals: workflow.approvals
      ? [...workflow.approvals].sort((left, right) => left.id.localeCompare(right.id))
      : undefined
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

function edgeSortKey(edge: WorkflowSpec["edges"][number]): string {
  return `${edge.source}->${edge.target}:${edge.id ?? ""}`;
}
