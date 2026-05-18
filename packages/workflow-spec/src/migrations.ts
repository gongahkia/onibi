import { assertValidWorkflowSpec } from "./validate.js";
import { workflowSchemaVersion } from "./types.js";
import type { WorkflowSpec } from "./types.js";

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

function readSchemaVersion(input: unknown): string | undefined {
  if (!input || typeof input !== "object" || !("schemaVersion" in input)) {
    return undefined;
  }

  const value = (input as { readonly schemaVersion?: unknown }).schemaVersion;
  return typeof value === "string" ? value : undefined;
}
