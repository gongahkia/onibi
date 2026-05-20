import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  gmailReceiptsToSheetsWorkflowFixture,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import { SqliteSecretStore, SqliteWorkflowStore } from "../src/index.js";
import type {
  WorkflowArtifactManifestRecord,
  WorkflowAuditRecord,
  WorkflowRunRecord
} from "@kelpclaw/workflow-spec";

describe("sqlite workflow store", () => {
  it("persists workflow revisions, runs, audit records, and manifests across restarts", async () => {
    const databasePath = join(await mkdtemp(join(tmpdir(), "kelpclaw-store-")), "workflow.db");
    const validation = validateWorkflowSpec(gmailReceiptsToSheetsWorkflowFixture);
    if (!validation.ok) {
      throw new Error("Fixture workflow is invalid.");
    }

    const store = new SqliteWorkflowStore({ databasePath });
    store.saveWorkflow(validation.workflow, validation);
    const approved = store.approveWorkflow(validation.workflow.id, "owner@example.com");
    const run = store.saveRun(runRecord(approved.id));
    const audit = store.saveAuditRecord(auditRecord(approved.id, run.id));
    const manifest = store.saveArtifactManifest(artifactManifest(approved.id));

    const rehydrated = new SqliteWorkflowStore({ databasePath });

    expect(rehydrated.getWorkflow(validation.workflow.id)?.approvedRevisions).toHaveLength(1);
    expect(rehydrated.getApprovedRevision(approved.id)?.workflow.id).toBe(validation.workflow.id);
    expect(rehydrated.getRun(run.id)?.events[0]?.correlationId).toBe("corr.store-test");
    expect(rehydrated.listRunEvents(run.id)).toHaveLength(1);
    expect(rehydrated.listAuditRecords(validation.workflow.id)).toEqual([audit]);
    expect(rehydrated.getArtifactManifest(manifest.id)).toEqual(manifest);
    expect(() =>
      rehydrated.saveArtifactManifest({ ...manifest, manifestChecksum: driftHash })
    ).toThrow(/Immutable|immutable/);
  }, 15_000);
});

describe("sqlite secret store", () => {
  it("persists encrypted secrets without exposing values in metadata", async () => {
    const databasePath = join(await mkdtemp(join(tmpdir(), "kelpclaw-secrets-")), "secrets.db");
    const store = new SqliteSecretStore({
      databasePath,
      masterKey: "phase-8-test-master-key"
    });

    const metadata = store.putSecret("email.smtp.default", "smtp-password");
    const rehydrated = new SqliteSecretStore({
      databasePath,
      masterKey: "phase-8-test-master-key"
    });
    const wrongKey = new SqliteSecretStore({
      databasePath,
      masterKey: "wrong-master-key"
    });

    expect(metadata).toMatchObject({ name: "email.smtp.default" });
    expect(rehydrated.listSecrets()).toEqual([
      expect.objectContaining({ name: "email.smtp.default" })
    ]);
    await expect(rehydrated.getSecretValue("email.smtp.default")).resolves.toBe("smtp-password");
    await expect(wrongKey.getSecretValue("email.smtp.default")).rejects.toThrow();
    expect(JSON.stringify(rehydrated.listSecrets())).not.toContain("smtp-password");
  });
});

const checksum = `sha256:${"a".repeat(64)}`;
const driftHash = `sha256:${"b".repeat(64)}`;

function runRecord(approvedRevisionId: string): WorkflowRunRecord {
  return {
    id: "run.workflow.gmail-receipts-to-sheets.r1.store",
    workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
    approvedRevisionId,
    revision: 1,
    status: "succeeded",
    createdAt: "2026-05-18T02:00:00.000Z",
    startedAt: "2026-05-18T02:00:00.000Z",
    finishedAt: "2026-05-18T02:00:01.000Z",
    events: [
      {
        id: "event.run.finished",
        timestamp: "2026-05-18T02:00:01.000Z",
        level: "info",
        severity: "info",
        kind: "run.lifecycle",
        workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
        revisionId: approvedRevisionId,
        runId: "run.workflow.gmail-receipts-to-sheets.r1.store",
        correlationId: "corr.store-test",
        message: "NanoClaw run finished."
      }
    ],
    result: null
  };
}

function auditRecord(approvedRevisionId: string, runId: string): WorkflowAuditRecord {
  return {
    id: "audit.run.completed.store",
    workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
    revisionId: approvedRevisionId,
    runId,
    correlationId: "corr.store-test",
    timestamp: "2026-05-18T02:00:01.000Z",
    action: "run.completed",
    actor: "system",
    summary: "Run completed."
  };
}

function artifactManifest(approvedRevisionId: string): WorkflowArtifactManifestRecord {
  return {
    id: "manifest.workflow.gmail-receipts-to-sheets.r1",
    workflowId: gmailReceiptsToSheetsWorkflowFixture.id,
    revisionId: approvedRevisionId,
    createdAt: "2026-05-18T02:00:00.000Z",
    manifestChecksum: checksum,
    artifacts: [
      {
        path: "generated/manifest.json",
        checksum,
        contentType: "application/json"
      }
    ]
  };
}
