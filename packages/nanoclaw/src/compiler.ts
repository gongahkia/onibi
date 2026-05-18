import { WorkflowValidationError, validateWorkflowSpec } from "@kelpclaw/workflow-spec";
import type { CompiledDag, CompiledDagNode } from "./types.js";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";

export function compileWorkflowDag(input: WorkflowSpec): CompiledDag {
  const validation = validateWorkflowSpec(input);
  if (!validation.ok) {
    throw new WorkflowValidationError(validation.errors);
  }

  const workflow = validation.workflow;
  const dependencies = new Map(workflow.nodes.map((node) => [node.id, new Set<string>()]));
  const dependents = new Map(workflow.nodes.map((node) => [node.id, new Set<string>()]));

  for (const edge of workflow.edges) {
    dependencies.get(edge.target)?.add(edge.source);
    dependents.get(edge.source)?.add(edge.target);
  }

  const nodes = new Map<string, CompiledDagNode>();
  for (const node of workflow.nodes) {
    nodes.set(node.id, {
      id: node.id,
      label: node.label,
      docker: node.docker,
      dependencies: [...(dependencies.get(node.id) ?? [])].sort(),
      dependents: [...(dependents.get(node.id) ?? [])].sort()
    });
  }

  return {
    workflowId: workflow.metadata.id,
    nodes,
    order: topologicalOrder(nodes),
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
