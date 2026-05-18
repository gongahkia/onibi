import { describe, expect, it } from "vitest";
import { createDeterministicHarness, runStaticFixture } from "../src/index.js";

describe("deterministic testing harness", () => {
  it("runs the static workflow fixture through NanoClaw's mock runner", async () => {
    const result = await runStaticFixture();

    expect(result.status).toBe("succeeded");
    expect(result.nodeResults.map((node) => node.nodeId)).toEqual([
      "collect-brief",
      "draft-copy",
      "owner-approval",
      "send-email"
    ]);
  });

  it("provides isolated fake adapters", async () => {
    const first = createDeterministicHarness();
    const second = createDeterministicHarness();

    await first.adapters.get("adapter.email.fake")?.invoke({
      adapterId: "adapter.email.fake",
      operation: "email.send",
      payload: { to: "owner@example.com" }
    });

    expect(first.adapters.get("adapter.email.fake")?.invocations).toHaveLength(1);
    expect(second.adapters.get("adapter.email.fake")?.invocations).toHaveLength(0);
  });
});
