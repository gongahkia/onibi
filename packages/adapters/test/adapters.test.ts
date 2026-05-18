import { describe, expect, it } from "vitest";
import {
  createDefaultFakeAdapters,
  fakeAdapterMetadata,
  requireFakeAdapter
} from "../src/index.js";

describe("adapter fakes", () => {
  it("declares all Phase 1 adapter surfaces as fake-only", () => {
    expect(fakeAdapterMetadata.map((adapter) => adapter.kind)).toEqual([
      "gmail",
      "sheets",
      "email",
      "whatsapp",
      "telegram"
    ]);
    expect(fakeAdapterMetadata.every((adapter) => adapter.live === false)).toBe(true);
  });

  it("records invocations deterministically", async () => {
    const adapter = requireFakeAdapter("adapter.email.fake");
    const result = await adapter.invoke({
      adapterId: "adapter.email.fake",
      operation: "email.send",
      payload: {
        to: "owner@example.com",
        subject: "Review"
      },
      idempotencyKey: "delivery-1"
    });

    expect(result).toEqual({
      adapterId: "adapter.email.fake",
      operation: "email.send",
      status: "recorded",
      receipt: {
        fake: true,
        sequence: 1,
        idempotencyKey: "delivery-1"
      }
    });
    expect(adapter.invocations).toHaveLength(1);
  });

  it("creates independent fake adapter registries", async () => {
    const first = createDefaultFakeAdapters();
    const second = createDefaultFakeAdapters();

    await requireFakeAdapter("adapter.telegram.fake", first).invoke({
      adapterId: "adapter.telegram.fake",
      operation: "message.send",
      payload: { chatId: "ops", text: "ready" }
    });

    expect(requireFakeAdapter("adapter.telegram.fake", first).invocations).toHaveLength(1);
    expect(requireFakeAdapter("adapter.telegram.fake", second).invocations).toHaveLength(0);
  });
});
