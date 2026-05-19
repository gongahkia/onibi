import { describe, expect, it } from "vitest";
import { createDefaultMockAdapters, requireMockAdapter } from "@kelpclaw/adapters";
import { createArtifactManifest, createGeneratedArtifact, decideReplay } from "@kelpclaw/codegen";
import { compileWorkflowDag, hashWorkflowDag } from "@kelpclaw/nanoclaw";
import { chooseSkillOrCodegen, clearPromotedSkillsForTests } from "@kelpclaw/skill-registry";
import {
  approvedGmailReceiptsToSheetsWorkflowFixture,
  createApprovedWorkflowFixture,
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import { createDeterministicPlannerBackend } from "@kelpclaw/api";

describe("enterprise deterministic evals", () => {
  it("plans common workflow prompts into valid workflow specs", async () => {
    const planner = createDeterministicPlannerBackend();

    for (const prompt of [
      "extract transaction details from Gmail receipts into Sheets",
      "monitor urgent support messages and send Telegram alerts",
      "scrape a custom public status page and summarize incidents"
    ]) {
      const workflow = await planner.plan({ prompt });
      expect(validateWorkflowSpec(workflow).ok).toBe(true);
    }
  });

  it("prefers existing deterministic skills over codegen when skill scores meet threshold", () => {
    clearPromotedSkillsForTests();

    expect(
      chooseSkillOrCodegen({
        nodeKind: "delivery",
        capability: "sheets-rows-append",
        adapterDependencies: ["adapter.sheets"],
        prompt: "append receipt rows to Google Sheets"
      }).kind
    ).toBe("skill");

    expect(
      chooseSkillOrCodegen({
        nodeKind: "skill",
        capability: "public-status-scrape",
        prompt: "scrape an arbitrary custom status page"
      }).kind
    ).toBe("codegen");
  });

  it("keeps approved DAG execution ordering stable across repeated compilation", () => {
    const first = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const second = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);

    expect(second.order).toEqual(first.order);
    expect(second.dagHash).toBe(first.dagHash);
  });

  it("replays persisted codegen artifacts only when manifests are unchanged", () => {
    const artifact = createGeneratedArtifact({
      path: "generated/node.ts",
      content: "export const ok = true;\n",
      contentType: "text/typescript"
    });
    const previous = createArtifactManifest({
      workflowId: "workflow.codegen-eval",
      generatedAt: "2026-05-18T00:00:00.000Z",
      artifacts: [artifact]
    });
    const drifted = createArtifactManifest({
      workflowId: "workflow.codegen-eval",
      generatedAt: "2026-05-18T00:00:00.000Z",
      artifacts: [
        createGeneratedArtifact({
          ...artifact,
          content: "export const ok = false;\n"
        })
      ]
    });

    expect(
      decideReplay(previous, previous, { mode: "reuse-if-unchanged", seed: "eval" }).action
    ).toBe("reuse");
    expect(decideReplay(previous, drifted, { mode: "fail-on-drift", seed: "eval" }).action).toBe(
      "fail"
    );
  });

  it("keeps adapter mock payloads deterministic for idempotency keys", async () => {
    const adapters = createDefaultMockAdapters();
    const email = requireMockAdapter("adapter.email", adapters);
    const invocation = {
      adapterId: "adapter.email",
      operation: "email.results.send",
      operationVersion: "1.0.0",
      payload: {
        to: "owner@example.com",
        subject: "Eval",
        body: "Done"
      },
      secretRefs: {
        "email.delivery": "secret:email.smtp.default"
      },
      context: {
        workflowId: "workflow.enterprise-eval",
        nodeId: "deliver-results",
        runId: "run.enterprise-eval",
        attempt: 1
      },
      idempotencyKey: "enterprise-eval-delivery"
    };

    const first = await email.invoke(invocation);
    const second = await email.invoke(invocation);

    expect(second.providerMetadata.providerResponseId).toBe(
      first.providerMetadata.providerResponseId
    );
  });

  it("keeps enterprise regression fixtures valid and approval-hashable", () => {
    for (const workflow of [
      gmailReceiptsToSheetsWorkflowFixture,
      scheduledScrapingWorkflowFixture,
      timeSensitiveAlertDeliveryWorkflowFixture
    ]) {
      const validation = validateWorkflowSpec(workflow);
      expect(validation.ok).toBe(true);
      const approved = createApprovedWorkflowFixture(workflow, {
        frozenDagHash: hashWorkflowDag(workflow),
        nodeOrder: workflow.nodes.map((node) => node.id)
      });
      expect(validateWorkflowSpec(approved).ok).toBe(true);
    }
  });
});
