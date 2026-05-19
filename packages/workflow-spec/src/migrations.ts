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
