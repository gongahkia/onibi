import { createHash } from "node:crypto";
import {
  WorkflowValidationError,
  assertApprovedWorkflowSpec,
  stableJsonStringify
} from "@kelpclaw/workflow-spec";
import type { CompiledDag, CompiledDagNode, CompiledNodeInputBinding } from "./types.js";
import type { JsonRecord, WorkflowSpec } from "@kelpclaw/workflow-spec";

export function compileWorkflowDag(input: WorkflowSpec): CompiledDag {
  const workflow = assertApprovedWorkflowSpec(input);
  const dependencies = new Map(workflow.nodes.map((node) => [node.id, new Set<string>()]));
  const dependents = new Map(workflow.nodes.map((node) => [node.id, new Set<string>()]));
  const inputBindings = new Map(
    workflow.nodes.map((node) => [node.id, [] as CompiledNodeInputBinding[]])
  );

  for (const edge of workflow.edges) {
    dependencies.get(edge.target.nodeId)?.add(edge.source.nodeId);
    dependents.get(edge.source.nodeId)?.add(edge.target.nodeId);
    inputBindings.get(edge.target.nodeId)?.push({
      edgeId: edge.id,
      inputPort: edge.target.port,
      source: edge.source
    });
  }

  const nodes = new Map<string, CompiledDagNode>();
  for (const node of workflow.nodes) {
    nodes.set(node.id, {
      id: node.id,
      kind: node.kind,
      label: node.label,
      description: node.description,
      inputs: node.inputs,
      outputs: node.outputs,
      config: node.config,
      runtime: node.runtime,
      determinism: node.determinism,
      skillId: node.skillId,
      adapterId: node.adapterId,
      adapterIds: node.adapterIds,
      adapterOperations: node.adapterOperations,
      secretRefs: node.secretRefs,
      codegen: node.codegen,
      dependencies: [...(dependencies.get(node.id) ?? [])].sort(),
      dependents: [...(dependents.get(node.id) ?? [])].sort(),
      inputBindings: [...(inputBindings.get(node.id) ?? [])].sort(compareInputBindings)
    });
  }

  const calculatedOrder = topologicalOrder(nodes);
  const approval = workflow.approval;
  if (!approval) {
    throw new WorkflowValidationError([
      {
        code: "WORKFLOW_EXECUTION_UNAPPROVED",
        message: "Workflow approval is required for NanoClaw compilation.",
        path: ["approval"]
      }
    ]);
  }

  const approvalOrder = approval.nodeOrder;
  if (approvalOrder.join("\n") !== calculatedOrder.join("\n")) {
    throw new WorkflowValidationError([
      {
        code: "WORKFLOW_EXECUTION_UNAPPROVED",
        message: "Approved node order does not match the compiled DAG.",
        path: ["approval", "nodeOrder"]
      }
    ]);
  }

  const dagHash = hashWorkflowDag(workflow);
  if (approval.frozenDagHash !== dagHash) {
    throw new WorkflowValidationError([
      {
        code: "WORKFLOW_EXECUTION_UNAPPROVED",
        message: "Approved DAG hash does not match the compiled workflow revision.",
        path: ["approval", "frozenDagHash"]
      }
    ]);
  }

  return {
    workflowId: workflow.id,
    revision: workflow.revision,
    approval,
    dagHash,
    nodes,
    edges: [...workflow.edges].sort((left, right) => left.id.localeCompare(right.id)),
    order: approvalOrder,
    source: workflow
  };
}

export function topologicalOrder(nodes: ReadonlyMap<string, CompiledDagNode>): readonly string[] {
  const indegrees = new Map([...nodes].map(([nodeId, node]) => [nodeId, node.dependencies.length]));
  const ready = [...indegrees]
    .filter(([, indegree]) => indegree === 0)
    .map(([nodeId]) => nodeId)
    .sort();
  const order: string[] = [];

  while (ready.length > 0) {
    const nodeId = ready.shift();
    if (nodeId === undefined) {
      break;
    }

    order.push(nodeId);
    const node = nodes.get(nodeId);
    for (const dependent of node?.dependents ?? []) {
      const nextIndegree = (indegrees.get(dependent) ?? 0) - 1;
      indegrees.set(dependent, nextIndegree);
      if (nextIndegree === 0) {
        ready.push(dependent);
        ready.sort();
      }
    }
  }

  if (order.length !== nodes.size) {
    throw new Error("Compiled workflow DAG contains a cycle.");
  }

  return order;
}

export function hashWorkflowDag(workflow: WorkflowSpec): string {
  return `sha256:${createHash("sha256")
    .update(stableJsonStringify(toHashableDag(workflow)), "utf8")
    .digest("hex")}`;
}

function toHashableDag(workflow: WorkflowSpec): JsonRecord {
  return {
    id: workflow.id,
    schemaVersion: workflow.schemaVersion,
    revision: workflow.revision,
    nodes: [...workflow.nodes].sort((left, right) => left.id.localeCompare(right.id)),
    edges: [...workflow.edges].sort((left, right) => left.id.localeCompare(right.id))
  } as unknown as JsonRecord;
}

function compareInputBindings(
  left: CompiledNodeInputBinding,
  right: CompiledNodeInputBinding
): number {
  return (
    left.inputPort.localeCompare(right.inputPort) ||
    left.source.nodeId.localeCompare(right.source.nodeId) ||
    left.source.port.localeCompare(right.source.port) ||
    left.edgeId.localeCompare(right.edgeId)
  );
}
