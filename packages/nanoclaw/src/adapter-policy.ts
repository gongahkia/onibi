import { builtinAdapterMetadata } from "@kelpclaw/adapters";
import { WorkflowValidationError } from "@kelpclaw/workflow-spec";
import type { Adapter, AdapterMetadata } from "@kelpclaw/adapters";
import type { CompiledDagNode } from "./types.js";
import type { WorkflowValidationIssue } from "@kelpclaw/workflow-spec";

export type AdapterRegistry = ReadonlyMap<string, Adapter>;

export function createAdapterMetadataRegistry(
  adapters: AdapterRegistry = defaultAdapterRegistry()
): ReadonlyMap<string, AdapterMetadata> {
  return new Map([...adapters].map(([adapterId, adapter]) => [adapterId, adapter.metadata]));
}

export function defaultAdapterRegistry(): AdapterRegistry {
  return new Map(
    builtinAdapterMetadata.map((metadata) => [
      metadata.id,
      {
        metadata,
        async invoke() {
          throw new Error(`Adapter '${metadata.id}' has metadata only.`);
        }
      }
    ])
  );
}

export function assertNodeAdapterPolicy(
  node: CompiledDagNode,
  adapterMetadata: ReadonlyMap<string, AdapterMetadata>
): void {
  const issues = validateNodeAdapterPolicy(node, adapterMetadata);
  if (issues.length > 0) {
    throw new WorkflowValidationError(issues);
  }
}

export function validateNodeAdapterPolicy(
  node: CompiledDagNode,
  adapterMetadata: ReadonlyMap<string, AdapterMetadata>
): readonly WorkflowValidationIssue[] {
  const issues: WorkflowValidationIssue[] = [];
  const operations = declaredAdapterOperations(node);
  const secretRefs = node.secretRefs ?? {};

  for (const operation of operations) {
    const metadata = adapterMetadata.get(operation.adapterId);
    if (!metadata) {
      issues.push({
        code: "WORKFLOW_ADAPTER_DECLARATION_INVALID",
        message: `Node '${node.id}' declares unknown adapter '${operation.adapterId}'.`,
        path: ["nodes", node.id, "adapterOperations"]
      });
      continue;
    }

    if (
      !metadata.operations.some(
        (candidate) =>
          candidate.name === operation.operation && candidate.version === operation.operationVersion
      )
    ) {
      issues.push({
        code: "WORKFLOW_ADAPTER_DECLARATION_INVALID",
        message: `Node '${node.id}' declares unsupported adapter operation '${operation.operation}' for '${operation.adapterId}'.`,
        path: ["nodes", node.id, "adapterOperations"]
      });
    }

    for (const secret of metadata.requiredSecrets) {
      if (!secretRefs[secret.name]) {
        issues.push({
          code: "WORKFLOW_ADAPTER_SECRET_MISSING",
          message: `Node '${node.id}' is missing secret reference '${secret.name}' for adapter '${metadata.id}'.`,
          path: ["nodes", node.id, "secretRefs", secret.name]
        });
      }
    }

    if (metadata.networkPolicy.mode === "declared") {
      const declaredHosts = declaredNetworkHosts(node);
      const missingHosts = metadata.networkPolicy.allowedHosts.filter(
        (host) => !declaredHosts.has(host)
      );
      if (missingHosts.length > 0) {
        issues.push({
          code: "WORKFLOW_ADAPTER_NETWORK_POLICY_INVALID",
          message: `Node '${node.id}' must declare network hosts for adapter '${metadata.id}': ${missingHosts.join(", ")}.`,
          path: ["nodes", node.id, "config", "allowedHosts"]
        });
      }
    }
  }

  return issues;
}

export function declaredAdapterOperations(node: CompiledDagNode): readonly {
  readonly adapterId: string;
  readonly operation: string;
  readonly operationVersion: string;
}[] {
  if (node.adapterOperations && node.adapterOperations.length > 0) {
    return node.adapterOperations;
  }

  if (node.kind === "delivery") {
    const channels = declaredDeliveryChannels(node);
    return [...channels].map((channel) => defaultDeliveryOperation(channel));
  }

  const adapterIds = [...(node.adapterId ? [node.adapterId] : []), ...(node.adapterIds ?? [])];
  return [...new Set(adapterIds)].map((adapterId) => ({
    adapterId,
    operation: "adapter.invoke",
    operationVersion: "1.0.0"
  }));
}

function declaredDeliveryChannels(node: CompiledDagNode): ReadonlySet<string> {
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

function defaultDeliveryOperation(channel: string) {
  switch (channel) {
    case "whatsapp":
      return {
        adapterId: "adapter.whatsapp",
        operation: "whatsapp.alert.send",
        operationVersion: "1.0.0"
      };
    case "telegram":
      return {
        adapterId: "adapter.telegram",
        operation: "telegram.alert.send",
        operationVersion: "1.0.0"
      };
    case "sheets":
      return {
        adapterId: "adapter.sheets",
        operation: "sheets.rows.append",
        operationVersion: "1.0.0"
      };
    case "email":
    default:
      return {
        adapterId: "adapter.email",
        operation: "email.results.send",
        operationVersion: "1.0.0"
      };
  }
}

function declaredNetworkHosts(node: CompiledDagNode): ReadonlySet<string> {
  const hosts = new Set<string>();
  for (const externalCall of node.determinism.externalCalls) {
    hosts.add(externalCall.replace(/^https?:\/\//u, ""));
  }

  const allowedHosts = node.config.allowedHosts;
  if (Array.isArray(allowedHosts)) {
    for (const host of allowedHosts) {
      if (typeof host === "string") {
        hosts.add(host);
      }
    }
  }

  return hosts;
}
