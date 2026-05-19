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

    errors.push(...validateRuntimeImagePolicy(node, index));
    if (node.kind === "codegen" && !node.codegen) {
      errors.push({
        code: "WORKFLOW_CODEGEN_METADATA_MISSING",
        message: `Codegen node '${node.id}' must include provenance, artifacts, sandbox, review, and replay metadata.`,
        path: ["nodes", index, "codegen"]
      });
    }
    if (node.kind === "codegen" && node.codegen) {
      errors.push(...validateCodegenMetadata(workflow, index));
    }
    if (node.kind === "delivery") {
      errors.push(...validateDeliveryChannelPolicy(node, index));
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

function validateRuntimeImagePolicy(
  node: WorkflowSpec["nodes"][number],
  nodeIndex: number
): WorkflowValidationIssue[] {
  if (runtimeImageIsPinned(node.runtime.image)) {
    return [];
  }

  return [
    {
      code: "WORKFLOW_RUNTIME_IMAGE_POLICY_INVALID",
      message: `Node '${node.id}' runtime image must be digest-addressed or pinned to an explicit non-latest tag.`,
      path: ["nodes", nodeIndex, "runtime", "image"]
    }
  ];
}

function validateDeliveryChannelPolicy(
  node: WorkflowSpec["nodes"][number],
  nodeIndex: number
): WorkflowValidationIssue[] {
  const channels = declaredDeliveryChannels(node);
  const adapterIds = new Set([
    ...(node.adapterId ? [node.adapterId] : []),
    ...(node.adapterIds ?? [])
  ]);
  const errors: WorkflowValidationIssue[] = [];

  for (const channel of ["whatsapp", "telegram"] as const) {
    const adapterId = `adapter.${channel}`;
    if (adapterIds.has(adapterId) && !channels.has(channel)) {
      errors.push({
        code: "WORKFLOW_DELIVERY_CHANNEL_POLICY_INVALID",
        message: `Delivery node '${node.id}' uses ${channel} adapter '${adapterId}' but does not declare '${channel}' in config.channels.`,
        path: ["nodes", nodeIndex, "config", "channels"]
      });
    }
  }

  return errors;
}

function declaredDeliveryChannels(node: WorkflowSpec["nodes"][number]): ReadonlySet<string> {
  const channels = new Set<string>();
  const configuredChannels = node.config.channels;
  if (Array.isArray(configuredChannels)) {
    for (const channel of configuredChannels) {
      if (typeof channel === "string") {
        channels.add(channel);
      }
    }
  }
  if (typeof node.config.channel === "string") {
    channels.add(node.config.channel);
  }
  if (channels.size === 0) {
    channels.add("email");
  }

  return channels;
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

  const unreviewedCodegenIndex = workflow.nodes.findIndex(
    (node) => node.kind === "codegen" && node.codegen?.review.status !== "approved"
  );
  if (unreviewedCodegenIndex >= 0) {
    const node = workflow.nodes[unreviewedCodegenIndex];
    return [
      {
        code: "WORKFLOW_CODEGEN_REVIEW_REQUIRED",
        message: `Codegen node '${node?.id ?? unreviewedCodegenIndex}' must be approved before execution.`,
        path: ["nodes", unreviewedCodegenIndex, "codegen", "review", "status"]
      }
    ];
  }

  return [];
}

function validateCodegenMetadata(
  workflow: WorkflowSpec,
  nodeIndex: number
): WorkflowValidationIssue[] {
  const node = workflow.nodes[nodeIndex];
  const codegen = node?.codegen;
  if (!node || !codegen) {
    return [];
  }

  const errors: WorkflowValidationIssue[] = [];
  const sourceArtifact = codegen.artifacts.find(
    (artifact) => artifact.path === codegen.provenance.artifactPath
  );
  if (!sourceArtifact || sourceArtifact.checksum !== codegen.provenance.artifactChecksum) {
    errors.push({
      code: "WORKFLOW_CODEGEN_ARTIFACT_DRIFT",
      message: `Codegen node '${node.id}' source artifact reference does not match provenance checksum.`,
      path: ["nodes", nodeIndex, "codegen", "artifacts"]
    });
  }

  const manifestArtifact = codegen.artifacts.find(
    (artifact) => artifact.path === codegen.dependencyManifest.path
  );
  if (!manifestArtifact || manifestArtifact.checksum !== codegen.dependencyManifest.checksum) {
    errors.push({
      code: "WORKFLOW_CODEGEN_ARTIFACT_DRIFT",
      message: `Codegen node '${node.id}' dependency manifest reference is missing or has drifted.`,
      path: ["nodes", nodeIndex, "codegen", "dependencyManifest"]
    });
  }

  if (!dependencyPolicyIsValid(codegen.dependencyManifest)) {
    errors.push({
      code: "WORKFLOW_CODEGEN_DEPENDENCY_POLICY_INVALID",
      message: `Codegen node '${node.id}' dependency manifest must use pinned dependencies and an explicit install policy.`,
      path: ["nodes", nodeIndex, "codegen", "dependencyManifest"]
    });
  }

  if (!sandboxPolicyIsValid(node.runtime.resources, codegen.sandbox)) {
    errors.push({
      code: "WORKFLOW_CODEGEN_SANDBOX_INVALID",
      message: `Codegen node '${node.id}' sandbox policy must match runtime resources and declared network access.`,
      path: ["nodes", nodeIndex, "codegen", "sandbox"]
    });
  }

  return errors;
}

function dependencyPolicyIsValid(
  manifest: NonNullable<WorkflowSpec["nodes"][number]["codegen"]>["dependencyManifest"]
): boolean {
  const allDependencies = [...manifest.dependencies, ...manifest.devDependencies];
  if (manifest.packageManager === "none") {
    return (
      allDependencies.length === 0 &&
      manifest.installCommand.length === 0 &&
      manifest.path.length > 0
    );
  }

  return manifest.installCommand.length > 0 && allDependencies.every(isPinnedDependency);
}

function isPinnedDependency(dependency: string): boolean {
  const separator = dependency.lastIndexOf("@");
  return separator > 0 && separator < dependency.length - 1 && !dependency.endsWith("@latest");
}

function sandboxPolicyIsValid(
  resources: WorkflowSpec["nodes"][number]["runtime"]["resources"],
  sandbox: NonNullable<WorkflowSpec["nodes"][number]["codegen"]>["sandbox"]
): boolean {
  if (sandbox.network === "none" && sandbox.allowedHosts.length > 0) {
    return false;
  }
  if (sandbox.network === "declared" && sandbox.allowedHosts.length === 0) {
    return false;
  }

  return (
    sandbox.resources.cpu === resources.cpu && sandbox.resources.memoryMb === resources.memoryMb
  );
}

function runtimeImageIsPinned(image: string): boolean {
  if (/^.+@sha256:[a-f0-9]{64}$/u.test(image)) {
    return true;
  }

  const lastSlash = image.lastIndexOf("/");
  const lastColon = image.lastIndexOf(":");
  if (lastColon <= lastSlash || lastColon === image.length - 1) {
    return false;
  }

  const tag = image.slice(lastColon + 1);
  return tag !== "latest";
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
