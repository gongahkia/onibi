import { stableWorkflowStringify } from "./stable-json.js";
import type { WorkflowDiffLine, WorkflowNode, WorkflowSpec, WorkflowSpecDiff } from "./types.js";

export function createWorkflowSpecDiff(before: WorkflowSpec, after: WorkflowSpec): WorkflowSpecDiff {
  const beforeJson = stableWorkflowStringify(before);
  const afterJson = stableWorkflowStringify(after);
  const lines = diffTextLines(beforeJson.split("\n"), afterJson.split("\n"));

  return {
    changed: beforeJson !== afterJson,
    summary: summarizeWorkflowChanges(before, after),
    lines
  };
}

export function diffTextLines(
  beforeLines: readonly string[],
  afterLines: readonly string[]
): readonly WorkflowDiffLine[] {
  let prefix = 0;
  while (
    prefix < beforeLines.length &&
    prefix < afterLines.length &&
    beforeLines[prefix] === afterLines[prefix]
  ) {
    prefix += 1;
  }

  let suffix = 0;
  while (
    suffix < beforeLines.length - prefix &&
    suffix < afterLines.length - prefix &&
    beforeLines[beforeLines.length - 1 - suffix] === afterLines[afterLines.length - 1 - suffix]
  ) {
    suffix += 1;
  }

  const output: WorkflowDiffLine[] = [];
  for (const text of beforeLines.slice(0, prefix)) {
    output.push({ kind: "same", text });
  }
  for (const text of beforeLines.slice(prefix, beforeLines.length - suffix)) {
    output.push({ kind: "removed", text });
  }
  for (const text of afterLines.slice(prefix, afterLines.length - suffix)) {
    output.push({ kind: "added", text });
  }
  if (suffix > 0) {
    for (const text of beforeLines.slice(beforeLines.length - suffix)) {
      output.push({ kind: "same", text });
    }
  }

  return output;
}

export function summarizeWorkflowChanges(
  before: WorkflowSpec,
  after: WorkflowSpec
): readonly string[] {
  const summary: string[] = [];
  const beforeNodes = new Map(before.nodes.map((node) => [node.id, node]));
  const afterNodes = new Map(after.nodes.map((node) => [node.id, node]));
  const beforeEdges = new Set(before.edges.map((edge) => edge.id));
  const afterEdges = new Set(after.edges.map((edge) => edge.id));

  const addedNodes = [...afterNodes.keys()].filter((nodeId) => !beforeNodes.has(nodeId));
  const removedNodes = [...beforeNodes.keys()].filter((nodeId) => !afterNodes.has(nodeId));
  const changedNodes = [...afterNodes.values()].filter((node) =>
    hasNodeChanged(beforeNodes.get(node.id), node)
  );
  const addedEdges = [...afterEdges].filter((edgeId) => !beforeEdges.has(edgeId));
  const removedEdges = [...beforeEdges].filter((edgeId) => !afterEdges.has(edgeId));

  if (addedNodes.length > 0) {
    summary.push(`Added ${addedNodes.length} node${addedNodes.length === 1 ? "" : "s"}.`);
  }
  if (removedNodes.length > 0) {
    summary.push(`Removed ${removedNodes.length} node${removedNodes.length === 1 ? "" : "s"}.`);
  }
  if (changedNodes.length > 0) {
    summary.push(`Changed ${changedNodes.length} node${changedNodes.length === 1 ? "" : "s"}.`);
  }
  if (addedEdges.length > 0) {
    summary.push(`Added ${addedEdges.length} edge${addedEdges.length === 1 ? "" : "s"}.`);
  }
  if (removedEdges.length > 0) {
    summary.push(`Removed ${removedEdges.length} edge${removedEdges.length === 1 ? "" : "s"}.`);
  }
  if (before.revision !== after.revision) {
    summary.push(`Revision ${before.revision} -> ${after.revision}.`);
  }
  if (before.approval?.frozenDagHash !== after.approval?.frozenDagHash) {
    summary.push(after.approval ? "Frozen approval metadata changed." : "Approval metadata removed.");
  }

  return summary.length > 0 ? summary : ["No workflow changes."];
}

function hasNodeChanged(before: WorkflowNode | undefined, after: WorkflowNode): boolean {
  if (!before) {
    return false;
  }

  return JSON.stringify(before) !== JSON.stringify(after);
}
