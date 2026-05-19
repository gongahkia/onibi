import { describe, expect, it } from "vitest";
import {
  approvedGmailReceiptsToSheetsWorkflowFixture,
  createApprovedWorkflowFixture,
  createWorkflowEdge,
  createWorkflowNode,
  createWorkflowSpec,
  createWorkflowSpecDiff,
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
  withConfig,
  workflowIdFromPrompt,
  workflowAuditRecordSchema,
  workflowObservabilityEventSchema,
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

  it("rejects undeclared secondary push delivery channels", () => {
    const result = validateWorkflowSpec(
      withConfig(timeSensitiveAlertDeliveryWorkflowFixture, "send-alert", {
        channel: "email"
      })
    );

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_DELIVERY_CHANNEL_POLICY_INVALID",
        "WORKFLOW_DELIVERY_CHANNEL_POLICY_INVALID"
      ]);
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

  it("rejects codegen artifact drift, unsafe dependencies, and sandbox mismatches", () => {
    const sourceNode = scheduledScrapingWorkflowFixture.nodes.find(
      (node) => node.id === "scrape-status-page"
    );
    if (!sourceNode?.codegen) {
      throw new Error("Scheduled scraping fixture is missing its codegen node.");
    }
    const sourceCodegen = sourceNode.codegen;

    const result = validateWorkflowSpec({
      ...scheduledScrapingWorkflowFixture,
      nodes: scheduledScrapingWorkflowFixture.nodes.map((node) =>
        node.id === sourceNode.id
          ? {
              ...node,
              codegen: {
                ...sourceCodegen,
                artifacts: sourceCodegen.artifacts.map((artifact) =>
                  artifact.path === sourceCodegen.provenance.artifactPath
                    ? {
                        ...artifact,
                        checksum: `sha256:${"d".repeat(64)}`
                      }
                    : artifact
                ),
                dependencyManifest: {
                  ...sourceCodegen.dependencyManifest,
                  packageManager: "npm",
                  dependencies: ["left-pad"],
                  installCommand: ["npm", "install"]
                },
                sandbox: {
                  ...sourceCodegen.sandbox,
                  network: "none"
                }
              }
            }
          : node
      )
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_CODEGEN_ARTIFACT_DRIFT",
        "WORKFLOW_CODEGEN_DEPENDENCY_POLICY_INVALID",
        "WORKFLOW_CODEGEN_SANDBOX_INVALID"
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

  it("blocks execution of unreviewed codegen nodes", () => {
    const approved = createApprovedWorkflowFixture(scheduledScrapingWorkflowFixture, {
      frozenDagHash: `sha256:${"a".repeat(64)}`,
      nodeOrder: scheduledScrapingWorkflowFixture.nodes.map((node) => node.id)
    });
    const result = validateWorkflowForExecution(approved);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual([
        "WORKFLOW_CODEGEN_REVIEW_REQUIRED"
      ]);
    }
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

describe("enterprise observability contracts", () => {
  it("validates structured event and audit records with correlation context", () => {
    const context = {
      workflowId: "workflow.gmail-receipts-to-sheets",
      revisionId: "approved.workflow.gmail-receipts-to-sheets.r1",
      runId: "run.workflow.gmail-receipts-to-sheets.r1.1",
      correlationId: "corr.phase7"
    };

    expect(
      workflowObservabilityEventSchema.parse({
        ...context,
        id: "event.run.started",
        timestamp: "2026-05-18T02:00:00.000Z",
        severity: "info",
        kind: "run.lifecycle",
        message: "NanoClaw run started.",
        metadata: {
          redacted: true
        }
      })
    ).toMatchObject({
      workflowId: context.workflowId,
      correlationId: context.correlationId
    });

    expect(
      workflowAuditRecordSchema.parse({
        ...context,
        id: "audit.workflow.approved",
        timestamp: "2026-05-18T02:00:00.000Z",
        action: "workflow.approved",
        actor: "owner@example.com",
        summary: "Approved workflow revision.",
        secretRefs: ["mock:gmail.oauth"]
      })
    ).toMatchObject({
      action: "workflow.approved",
      actor: "owner@example.com"
    });
  });
});

describe("workflow graph helpers", () => {
  it("creates valid default nodes and edges for OpenClaw editing", () => {
    const trigger = createWorkflowNode({
      id: "manual-trigger",
      kind: "trigger"
    });
    const delivery = createWorkflowNode({
      id: "send-result",
      kind: "delivery",
      inputs: {
        request: { type: "object", additionalProperties: true }
      }
    });
    const workflow = createWorkflowSpec({
      id: workflowIdFromPrompt("Send the result"),
      name: "Send Result",
      prompt: "Send the result",
      createdAt: "2026-05-18T00:00:00.000Z",
      nodes: [trigger, delivery],
      edges: [
        createWorkflowEdge({
          sourceNodeId: trigger.id,
          sourcePort: "request",
          targetNodeId: delivery.id,
          targetPort: "request"
        })
      ]
    });

    expect(validateWorkflowSpec(workflow).ok).toBe(true);
    expect(workflow.id).toBe("workflow.send-the-result");
  });

  it("creates stable workflow diffs for approval review", () => {
    const changed = {
      ...gmailReceiptsToSheetsWorkflowFixture,
      nodes: gmailReceiptsToSheetsWorkflowFixture.nodes.map((node) =>
        node.id === "normalize-receipts" ? { ...node, label: "Normalize Receipt Rows" } : node
      )
    };

    expect(createWorkflowSpecDiff(gmailReceiptsToSheetsWorkflowFixture, changed)).toMatchSnapshot();
  });
});
