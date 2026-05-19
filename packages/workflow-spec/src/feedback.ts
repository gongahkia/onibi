import { validateWorkflowSpec } from "./validate.js";
import { stableJsonStringify } from "./stable-json.js";
import type {
  JsonValue,
  WorkflowGraphChange,
  WorkflowGraphDiff,
  WorkflowPlannerFeedback,
  WorkflowPlannerSuggestion,
  WorkflowSpec,
  WorkflowTaskRoute,
  WorkflowValidationIssue
} from "./types.js";

export interface WorkflowGraphDiffInput {
  readonly id: string;
  readonly baseWorkflow: WorkflowSpec;
  readonly editedWorkflow: WorkflowSpec;
  readonly createdAt?: string | undefined;
}

export interface WorkflowPlannerFeedbackInput {
  readonly id: string;
  readonly graphDiff: WorkflowGraphDiff;
  readonly route: WorkflowTaskRoute;
  readonly createdAt?: string | undefined;
  readonly extraSuggestions?: readonly WorkflowPlannerSuggestion[] | undefined;
}

export function createWorkflowGraphDiff(input: WorkflowGraphDiffInput): WorkflowGraphDiff {
  const changes = [
    ...diffNodes(input.baseWorkflow, input.editedWorkflow),
    ...diffEdges(input.baseWorkflow, input.editedWorkflow)
  ];
  const validation = validateWorkflowSpec(input.editedWorkflow);

  return {
    id: input.id,
    workflowId: input.editedWorkflow.id,
    baseRevision: input.baseWorkflow.revision,
    editedRevision: input.editedWorkflow.revision,
    createdAt: input.createdAt ?? new Date().toISOString(),
    summary: summarizeGraphChanges(changes, validation.ok ? [] : validation.errors),
    changes,
    validation
  };
}

export function createWorkflowPlannerFeedback(
  input: WorkflowPlannerFeedbackInput
): WorkflowPlannerFeedback {
  const validationIssues = input.graphDiff.validation.ok ? [] : input.graphDiff.validation.errors;
  const suggestions = [
    ...validationIssues.map((issue, index) =>
      suggestionFromValidationIssue(input.graphDiff, issue, index)
    ),
    ...(input.extraSuggestions ?? [])
  ];

  return {
    id: input.id,
    workflowId: input.graphDiff.workflowId,
    graphDiffId: input.graphDiff.id,
    route: input.route,
    createdAt: input.createdAt ?? new Date().toISOString(),
    status:
      validationIssues.length > 0
        ? "blocked"
        : suggestions.some((suggestion) => suggestion.conflict !== "safe")
          ? "warnings"
          : "ready",
    suggestions,
    issues: validationIssues
  };
}

function diffNodes(base: WorkflowSpec, edited: WorkflowSpec): readonly WorkflowGraphChange[] {
  const baseNodes = new Map(base.nodes.map((node, index) => [node.id, { node, index }]));
  const editedNodes = new Map(edited.nodes.map((node, index) => [node.id, { node, index }]));
  const changes: WorkflowGraphChange[] = [];

  for (const [nodeId, { node, index }] of baseNodes) {
    const editedNode = editedNodes.get(nodeId);
    if (!editedNode) {
      changes.push({
        id: `change.node.removed.${nodeId}`,
        kind: "node.removed",
        elementId: nodeId,
        path: ["nodes", index],
        before: node as unknown as JsonValue
      });
      continue;
    }

    const baseCanvas = stableStringify(node.config.canvas);
    const editedCanvas = stableStringify(editedNode.node.config.canvas);
    const baseWithoutCanvas = stableStringify({
      ...node,
      config: {
        ...node.config,
        canvas: undefined
      }
    });
    const editedWithoutCanvas = stableStringify({
      ...editedNode.node,
      config: {
        ...editedNode.node.config,
        canvas: undefined
      }
    });

    if (baseCanvas !== editedCanvas) {
      changes.push({
        id: `change.node.moved.${nodeId}`,
        kind: "node.moved",
        elementId: nodeId,
        path: ["nodes", editedNode.index, "config", "canvas"],
        before: node.config.canvas,
        after: editedNode.node.config.canvas
      });
    }
    if (baseWithoutCanvas !== editedWithoutCanvas) {
      changes.push({
        id: `change.node.edited.${nodeId}`,
        kind: "node.edited",
        elementId: nodeId,
        path: ["nodes", editedNode.index],
        before: node as unknown as JsonValue,
        after: editedNode.node as unknown as JsonValue
      });
    }
  }

  for (const [nodeId, { node, index }] of editedNodes) {
    if (!baseNodes.has(nodeId)) {
      changes.push({
        id: `change.node.added.${nodeId}`,
        kind: "node.added",
        elementId: nodeId,
        path: ["nodes", index],
        after: node as unknown as JsonValue
      });
    }
  }

  return changes.sort(compareChanges);
}

function diffEdges(base: WorkflowSpec, edited: WorkflowSpec): readonly WorkflowGraphChange[] {
  const baseEdges = new Map(base.edges.map((edge, index) => [edge.id, { edge, index }]));
  const editedEdges = new Map(edited.edges.map((edge, index) => [edge.id, { edge, index }]));
  const changes: WorkflowGraphChange[] = [];

  for (const [edgeId, { edge, index }] of baseEdges) {
    const editedEdge = editedEdges.get(edgeId);
    if (!editedEdge) {
      changes.push({
        id: `change.edge.removed.${edgeId}`,
        kind: "edge.removed",
        elementId: edgeId,
        path: ["edges", index],
        before: edge as unknown as JsonValue
      });
      continue;
    }

    if (stableStringify(edge) !== stableStringify(editedEdge.edge)) {
      changes.push({
        id: `change.edge.reconnected.${edgeId}`,
        kind: "edge.reconnected",
        elementId: edgeId,
        path: ["edges", editedEdge.index],
        before: edge as unknown as JsonValue,
        after: editedEdge.edge as unknown as JsonValue
      });
    }
  }

  for (const [edgeId, { edge, index }] of editedEdges) {
    if (!baseEdges.has(edgeId)) {
      changes.push({
        id: `change.edge.added.${edgeId}`,
        kind: "edge.added",
        elementId: edgeId,
        path: ["edges", index],
        after: edge as unknown as JsonValue
      });
    }
  }

  return changes.sort(compareChanges);
}

function summarizeGraphChanges(
  changes: readonly WorkflowGraphChange[],
  issues: readonly WorkflowValidationIssue[]
): readonly string[] {
  const summary: string[] = [];
  const counts = new Map<string, number>();
  for (const change of changes) {
    counts.set(change.kind, (counts.get(change.kind) ?? 0) + 1);
  }
  for (const [kind, count] of [...counts.entries()].sort()) {
    summary.push(`${kind}: ${count}`);
  }
  if (issues.length > 0) {
    summary.push(`validation issues: ${issues.length}`);
  }
  if (summary.length === 0) {
    summary.push("No graph changes detected.");
  }

  return summary;
}

function suggestionFromValidationIssue(
  graphDiff: WorkflowGraphDiff,
  issue: WorkflowValidationIssue,
  index: number
): WorkflowPlannerSuggestion {
  const target = issueTarget(graphDiff, issue);

  return {
    id: `suggestion.${graphDiff.id}.${index}`,
    status: "suggested",
    conflict: "needs-repair",
    target,
    title: issue.code,
    message: issue.message,
    issues: [issue]
  };
}

function issueTarget(
  graphDiff: WorkflowGraphDiff,
  issue: WorkflowValidationIssue
): WorkflowPlannerSuggestion["target"] {
  const [collection, index] = issue.path;
  if (collection === "nodes" && typeof index === "number" && graphDiff.validation.ok) {
    return { kind: "node" };
  }
  if (collection === "nodes" && typeof index === "number") {
    const workflow = graphDiff.validation.ok ? graphDiff.validation.workflow : null;
    return {
      kind: "node",
      id: workflow?.nodes[index]?.id
    };
  }
  if (collection === "edges" && typeof index === "number") {
    return {
      kind: "edge"
    };
  }

  return { kind: "workflow" };
}

function compareChanges(left: WorkflowGraphChange, right: WorkflowGraphChange): number {
  return left.kind.localeCompare(right.kind) || left.elementId.localeCompare(right.elementId);
}

function stableStringify(value: unknown): string {
  return stableJsonStringify((value ?? null) as never);
}
