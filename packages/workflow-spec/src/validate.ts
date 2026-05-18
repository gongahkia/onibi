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

export function validateWorkflowForExecution(input: unknown): WorkflowValidationResult {
  const validation = validateWorkflowSpec(input);
  if (!validation.ok) {
    return validation;
  }

  const approvalErrors = validateApprovalForExecution(validation.workflow);
  if (approvalErrors.length > 0) {
    return { ok: false, errors: approvalErrors };
  }

  return validation;
}

export function assertValidWorkflowSpec(input: unknown): WorkflowSpec {
  const result = validateWorkflowSpec(input);
  if (!result.ok) {
    throw new WorkflowValidationError(result.errors);
  }

  return result.workflow;
}

export function assertApprovedWorkflowSpec(input: unknown): WorkflowSpec {
  const result = validateWorkflowForExecution(input);
  if (!result.ok) {
    throw new WorkflowValidationError(result.errors);
  }

  return result.workflow;
}

function validateWorkflowSemantics(workflow: WorkflowSpec): WorkflowValidationIssue[] {
  const errors: WorkflowValidationIssue[] = [];
  const nodeIds = new Set<string>();
  const duplicateIds = new Set<string>();
  const nodesById = new Map(workflow.nodes.map((node) => [node.id, node]));

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

    if (node.kind === "codegen" && !node.codegen) {
      errors.push({
        code: "WORKFLOW_CODEGEN_METADATA_MISSING",
        message: `Codegen node '${node.id}' must include provenance and replay metadata.`,
        path: ["nodes", index, "codegen"]
      });
    }
  });

  workflow.edges.forEach((edge, index) => {
    const sourceNode = nodesById.get(edge.source.nodeId);
    const targetNode = nodesById.get(edge.target.nodeId);

    if (!sourceNode) {
      errors.push({
        code: "WORKFLOW_EDGE_SOURCE_NODE_MISSING",
        message: `Workflow edge source node '${edge.source.nodeId}' does not exist.`,
        path: ["edges", index, "source", "nodeId"]
      });
    } else if (!(edge.source.port in sourceNode.outputs)) {
      errors.push({
        code: "WORKFLOW_EDGE_SOURCE_PORT_INVALID",
        message: `Workflow edge source port '${edge.source.port}' does not exist on node '${sourceNode.id}'.`,
        path: ["edges", index, "source", "port"]
      });
    }

    if (!targetNode) {
      errors.push({
        code: "WORKFLOW_EDGE_TARGET_NODE_MISSING",
        message: `Workflow edge target node '${edge.target.nodeId}' does not exist.`,
        path: ["edges", index, "target", "nodeId"]
      });
    } else if (!(edge.target.port in targetNode.inputs)) {
      errors.push({
        code: "WORKFLOW_EDGE_TARGET_PORT_INVALID",
        message: `Workflow edge target port '${edge.target.port}' does not exist on node '${targetNode.id}'.`,
        path: ["edges", index, "target", "port"]
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

function validateApprovalForExecution(workflow: WorkflowSpec): WorkflowValidationIssue[] {
  if (!workflow.approval || workflow.approval.frozenRevision !== workflow.revision) {
    return [
      {
        code: "WORKFLOW_EXECUTION_UNAPPROVED",
        message: `Workflow '${workflow.id}' revision ${workflow.revision} is not approved for execution.`,
        path: ["approval"]
      }
    ];
  }

  const nodeIds = new Set(workflow.nodes.map((node) => node.id));
  const approvalOrder = new Set(workflow.approval.nodeOrder);
  if (
    approvalOrder.size !== nodeIds.size ||
    [...nodeIds].some((nodeId) => !approvalOrder.has(nodeId))
  ) {
    return [
      {
        code: "WORKFLOW_EXECUTION_UNAPPROVED",
        message: "Workflow approval does not freeze the current DAG node order.",
        path: ["approval", "nodeOrder"]
      }
    ];
  }

  return [];
}

function hasCycle(workflow: WorkflowSpec): boolean {
  const indegrees = new Map(workflow.nodes.map((node) => [node.id, 0]));
  const outgoing = new Map(workflow.nodes.map((node) => [node.id, [] as string[]]));

  for (const edge of workflow.edges) {
    outgoing.get(edge.source.nodeId)?.push(edge.target.nodeId);
    indegrees.set(edge.target.nodeId, (indegrees.get(edge.target.nodeId) ?? 0) + 1);
  }

  const ready = [...indegrees.entries()]
    .filter(([, indegree]) => indegree === 0)
    .map(([nodeId]) => nodeId)
    .sort();
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
        ready.sort();
      }
    }
  }

  return visited !== workflow.nodes.length;
}
