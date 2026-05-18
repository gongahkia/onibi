import { workflowSpecSchema } from "./schema.js";
import type { WorkflowSpec, WorkflowValidationIssue, WorkflowValidationResult } from "./types.js";

export class WorkflowValidationError extends Error {
  public readonly issues: readonly WorkflowValidationIssue[];

  public constructor(issues: readonly WorkflowValidationIssue[]) {
    super(issues.map((issue) => issue.code).join(", "));
    this.name = "WorkflowValidationError";
    this.issues = issues;
  }
}

export function validateWorkflowSpec(input: unknown): WorkflowValidationResult {
  const parsed = workflowSpecSchema.safeParse(input);
  if (!parsed.success) {
    return {
      ok: false,
      errors: parsed.error.issues.map((issue) => ({
        code: "WORKFLOW_SCHEMA_INVALID",
        message: issue.message,
        path: issue.path.map((segment) => (typeof segment === "number" ? segment : String(segment)))
      }))
    };
  }

  const workflow = parsed.data satisfies WorkflowSpec;
  const semanticErrors = validateWorkflowSemantics(workflow);
  if (semanticErrors.length > 0) {
    return { ok: false, errors: semanticErrors };
  }

  return { ok: true, workflow };
}

export function assertValidWorkflowSpec(input: unknown): WorkflowSpec {
  const result = validateWorkflowSpec(input);
  if (!result.ok) {
    throw new WorkflowValidationError(result.errors);
  }

  return result.workflow;
}

function validateWorkflowSemantics(workflow: WorkflowSpec): WorkflowValidationIssue[] {
  const errors: WorkflowValidationIssue[] = [];
  const nodeIds = new Set<string>();
  const duplicateIds = new Set<string>();

  workflow.nodes.forEach((node, index) => {
    if (nodeIds.has(node.id)) {
      duplicateIds.add(node.id);
      errors.push({
        code: "WORKFLOW_NODE_ID_DUPLICATE",
        message: `Duplicate workflow node id '${node.id}'.`,
        path: ["nodes", index, "id"]
      });
    }
    nodeIds.add(node.id);
  });

  workflow.edges.forEach((edge, index) => {
    if (!nodeIds.has(edge.source)) {
      errors.push({
        code: "WORKFLOW_EDGE_SOURCE_MISSING",
        message: `Workflow edge source '${edge.source}' does not exist.`,
        path: ["edges", index, "source"]
      });
    }

    if (!nodeIds.has(edge.target)) {
      errors.push({
        code: "WORKFLOW_EDGE_TARGET_MISSING",
        message: `Workflow edge target '${edge.target}' does not exist.`,
        path: ["edges", index, "target"]
      });
    }
  });

  if (duplicateIds.size === 0 && errors.length === 0 && hasCycle(workflow)) {
    errors.push({
      code: "WORKFLOW_DAG_CYCLE",
      message: "Workflow graph must be acyclic.",
      path: ["edges"]
    });
  }

  return errors;
}

function hasCycle(workflow: WorkflowSpec): boolean {
  const indegrees = new Map(workflow.nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(workflow.nodes.map((node) => [node.id, [] as string[]]));

  for (const edge of workflow.edges) {
    outgoing.get(edge.source)?.push(edge.target);
    indegrees.set(edge.target, (indegrees.get(edge.target) ?? 0) + 1);
  }

  const ready = [...indegrees.entries()]
    .filter(([, indegree]) => indegree === 0)
    .map(([nodeId]) => nodeId);
  let visited = 0;

  while (ready.length > 0) {
    const nodeId = ready.shift();
    if (nodeId === undefined) {
      break;
    }

    visited += 1;
    for (const target of outgoing.get(nodeId) ?? []) {
      const nextIndegree = (indegrees.get(target) ?? 0) - 1;
      indegrees.set(target, nextIndegree);
      if (nextIndegree === 0) {
        ready.push(target);
      }
    }
  }

  return visited !== workflow.nodes.length;
}
