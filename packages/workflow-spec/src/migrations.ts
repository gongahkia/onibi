import { assertValidWorkflowSpec } from "./validate.js";
import { workflowSchemaVersion } from "./types.js";
import type { WorkflowSpec } from "./types.js";

const adapterIdMappings = {
  "adapter.gmail.fake": "adapter.gmail",
  "adapter.sheets.fake": "adapter.sheets",
  "adapter.email.fake": "adapter.email",
  "adapter.whatsapp.fake": "adapter.whatsapp",
  "adapter.telegram.fake": "adapter.telegram"
} as const;

const secretRefMappings = {
  "mock:gmail.oauth": "secret:google.oauth.default",
  "mock:sheets.oauth": "secret:google.oauth.default",
  "mock:email.delivery": "secret:email.smtp.default",
  "mock:whatsapp.apiKey": "secret:whatsapp.cloud.default",
  "mock:telegram.botToken": "secret:telegram.bot.default"
} as const;

export class WorkflowMigrationError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "WorkflowMigrationError";
  }
}

export function migrateWorkflowToLatest(input: unknown): WorkflowSpec {
  const schemaVersion = readSchemaVersion(input);

  if (schemaVersion === workflowSchemaVersion) {
    return assertValidWorkflowSpec(input);
  }

  throw new WorkflowMigrationError(
    `Unsupported workflow schema version '${schemaVersion ?? "unknown"}'.`
  );
}

export function migrateWorkflowAdapterIdsToLive(workflow: WorkflowSpec): WorkflowSpec {
  return {
    ...workflow,
    nodes: workflow.nodes.map((node) => ({
      ...node,
      config: withAdapterHosts(
        node.config,
        adapterHostsForOperations(node.adapterOperations ?? [])
      ),
      determinism: {
        ...node.determinism,
        externalCalls: node.determinism.externalCalls.map(rewriteAdapterId)
      },
      ...(node.adapterId ? { adapterId: rewriteAdapterId(node.adapterId) } : {}),
      ...(node.adapterIds ? { adapterIds: node.adapterIds.map(rewriteAdapterId) } : {}),
      ...(node.adapterOperations
        ? {
            adapterOperations: node.adapterOperations.map((operation) => ({
              ...operation,
              adapterId: rewriteAdapterId(operation.adapterId)
            }))
          }
        : {}),
      ...(node.secretRefs
        ? {
            secretRefs: Object.fromEntries(
              Object.entries(node.secretRefs).map(([name, ref]) => [name, rewriteSecretRef(ref)])
            )
          }
        : {})
    }))
  };
}

function adapterHostsForOperations(
  operations: WorkflowSpec["nodes"][number]["adapterOperations"]
): readonly string[] {
  const hosts = new Set<string>();
  for (const operation of operations ?? []) {
    for (const host of hostsForAdapter(rewriteAdapterId(operation.adapterId))) {
      hosts.add(host);
    }
  }

  return [...hosts].sort();
}

function hostsForAdapter(adapterId: string): readonly string[] {
  switch (adapterId) {
    case "adapter.gmail":
      return ["oauth2.googleapis.com", "gmail.googleapis.com"];
    case "adapter.sheets":
      return ["oauth2.googleapis.com", "sheets.googleapis.com"];
    case "adapter.email":
      return ["smtp"];
    case "adapter.whatsapp":
      return ["graph.facebook.com"];
    case "adapter.telegram":
      return ["api.telegram.org"];
    default:
      return [];
  }
}

function mergeAllowedHosts(existing: unknown, additional: readonly string[]): readonly string[] {
  const hosts = new Set<string>();
  if (Array.isArray(existing)) {
    for (const host of existing) {
      if (typeof host === "string") {
        hosts.add(host);
      }
    }
  }
  for (const host of additional) {
    hosts.add(host);
  }

  return [...hosts].sort();
}

function withAdapterHosts<TConfig extends WorkflowSpec["nodes"][number]["config"]>(
  config: TConfig,
  hosts: readonly string[]
): TConfig {
  if (hosts.length === 0 && !Array.isArray(config.allowedHosts)) {
    return config;
  }

  return {
    ...config,
    allowedHosts: mergeAllowedHosts(config.allowedHosts, hosts)
  } as TConfig;
}

function readSchemaVersion(input: unknown): string | undefined {
  if (!input || typeof input !== "object" || !("schemaVersion" in input)) {
    return undefined;
  }

  const value = (input as { readonly schemaVersion?: unknown }).schemaVersion;
  return typeof value === "string" ? value : undefined;
}

function rewriteAdapterId(adapterId: string): string {
  return adapterIdMappings[adapterId as keyof typeof adapterIdMappings] ?? adapterId;
}

function rewriteSecretRef(secretRef: string): string {
  return secretRefMappings[secretRef as keyof typeof secretRefMappings] ?? secretRef;
}
