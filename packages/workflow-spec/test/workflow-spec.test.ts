import { describe, expect, it } from "vitest";
import {
  approvedGmailReceiptsToSheetsWorkflowFixture,
  cyclicWorkflowFixture,
  gmailReceiptsToSheetsWorkflowFixture,
  invalidEdgePortWorkflowFixture,
  migrateWorkflowToLatest,
  missingCodegenMetadataWorkflowFixture,
  missingEdgeTargetWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  stableWorkflowStringify,
  timeSensitiveAlertDeliveryWorkflowFixture,
  validateWorkflowForExecution,
  validateWorkflowSpec,
  workflowJsonSchema,
  workflowSchemaVersion
} from "../src/index.js";

describe("workflow spec validation", () => {
  it("accepts Phase 2 canonical workflow fixtures", () => {
    for (const workflow of [
      gmailReceiptsToSheetsWorkflowFixture,
      scheduledScrapingWorkflowFixture,
      timeSensitiveAlertDeliveryWorkflowFixture
    ]) {
      const result = validateWorkflowSpec(workflow);

      expect(result.ok).toBe(true);
      if (result.ok) {
        expect(result.workflow.schemaVersion).toBe(workflowSchemaVersion);
      }
    }
  });

  it("rejects missing edge targets with stable error codes", () => {
    const result = validateWorkflowSpec(missingEdgeTargetWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_EDGE_TARGET_NODE_MISSING"
      ]);
    }
  });

  it("rejects invalid edge ports with stable error codes", () => {
    const result = validateWorkflowSpec(invalidEdgePortWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_EDGE_SOURCE_PORT_INVALID"
      ]);
    }
  });

  it("rejects cyclic graphs with a stable error code", () => {
    const result = validateWorkflowSpec(cyclicWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual(["WORKFLOW_DAG_CYCLE"]);
    }
  });

  it("rejects duplicate node ids with a stable error code", () => {
    const result = validateWorkflowSpec({
      ...gmailReceiptsToSheetsWorkflowFixture,
      nodes: [
        gmailReceiptsToSheetsWorkflowFixture.nodes[0],
        { ...gmailReceiptsToSheetsWorkflowFixture.nodes[0], label: "Duplicate" }
      ],
      edges: []
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual(["WORKFLOW_NODE_ID_DUPLICATE"]);
    }
  });

  it("rejects codegen nodes without provenance and replay metadata", () => {
    const result = validateWorkflowSpec(missingCodegenMetadataWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_CODEGEN_METADATA_MISSING"
      ]);
    }
  });

  it("blocks execution until the current revision is approved", () => {
    const draft = validateWorkflowForExecution(gmailReceiptsToSheetsWorkflowFixture);
    expect(draft.ok).toBe(false);
    if (!draft.ok) {
      expect(draft.errors.map((error) => error.code)).toEqual(["WORKFLOW_EXECUTION_UNAPPROVED"]);
    }

    const approved = validateWorkflowForExecution(approvedGmailReceiptsToSheetsWorkflowFixture);
    expect(approved.ok).toBe(true);
  });

  it("serializes fixtures as stable canonical JSON snapshots", () => {
    expect(stableWorkflowStringify(gmailReceiptsToSheetsWorkflowFixture)).toMatchSnapshot();
    expect(stableWorkflowStringify(scheduledScrapingWorkflowFixture)).toMatchSnapshot();
    expect(stableWorkflowStringify(timeSensitiveAlertDeliveryWorkflowFixture)).toMatchSnapshot();
  });

  it("exports a JSON Schema document for generated workflow specs", () => {
    expect(workflowJsonSchema.$id).toBe(
      "https://kelpclaw.dev/schemas/workflow-spec.v1.schema.json"
    );
    expect(workflowJsonSchema.required).toEqual([
      "id",
      "schemaVersion",
      "name",
      "prompt",
      "revision",
      "nodes",
      "edges",
      "approval",
      "createdAt",
      "updatedAt"
    ]);
  });
});

describe("workflow migrations", () => {
  it("passes v1 workflows through the migration harness", () => {
    expect(migrateWorkflowToLatest(gmailReceiptsToSheetsWorkflowFixture).id).toBe(
      "workflow.gmail-receipts-to-sheets"
    );
  });

  it("rejects unsupported schema versions", () => {
    expect(() =>
      migrateWorkflowToLatest({
        ...gmailReceiptsToSheetsWorkflowFixture,
        schemaVersion: "0.9.0"
      })
    ).toThrow("Unsupported workflow schema version");
  });
});
