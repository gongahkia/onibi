import { describe, expect, it } from "vitest";
import { createDeterministicHarness, runStaticFixture } from "../src/index.js";

describe("deterministic testing harness", () => {
  it("runs the static workflow fixture through NanoClaw's mock runner", async () => {
    const result = await runStaticFixture();

    expect(result.status).toBe("succeeded");
    expect(result.nodeResults.map((node) => node.nodeId)).toEqual([
      "manual-trigger",
      "read-gmail-receipts",
      "normalize-receipts",
      "append-sheet-rows",
      "deliver-results-email"
    ]);
  });

  it("provides isolated fake adapters", async () => {
    const first = createDeterministicHarness();
    const second = createDeterministicHarness();

    await first.adapters.get("adapter.email.fake")?.invoke({
      adapterId: "adapter.email.fake",
      operation: "email.results.send",
      operationVersion: "1.0.0",
      payload: { to: "owner@example.com", subject: "Done", body: "Done", summary: {} },
      secretRefs: {
        "email.delivery": "mock:email.delivery"
      },
      context: {
        workflowId: "workflow.test",
        nodeId: "email",
        runId: "run.test",
        attempt: 1
      }
    });

    expect(first.adapters.get("adapter.email.fake")?.invocations).toHaveLength(1);
    expect(second.adapters.get("adapter.email.fake")?.invocations).toHaveLength(0);
  });
});
