import { describe, expect, it } from "vitest";
import {
  AdapterCredentialError,
  builtinAdapterMetadata,
  createDefaultMockAdapters,
  receiptExtractionToSheetsFixture,
  requireMockAdapter,
  validateAdapterCredentialRefs
} from "../src/index.js";
import type { AdapterInvocation } from "../src/index.js";

const context = {
  workflowId: "workflow.gmail-receipts-to-sheets",
  nodeId: "deliver-results-email",
  runId: "run.phase6",
  attempt: 1
} as const;

describe("adapter metadata", () => {
  it("declares all Phase 6 adapter families with schemas, policy, secrets, and fixtures", () => {
    expect(builtinAdapterMetadata.map((adapter) => adapter.kind)).toEqual([
      "gmail",
      "sheets",
      "email",
      "whatsapp",
      "telegram"
    ]);
    expect(builtinAdapterMetadata.every((adapter) => adapter.live === false)).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.version === "1.0.0")).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.operations.length > 0)).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.requiredSecrets.length > 0)).toBe(
      true
    );
    expect(builtinAdapterMetadata.every((adapter) => adapter.networkPolicy.mode === "none")).toBe(
      true
    );
    expect(builtinAdapterMetadata.every((adapter) => adapter.fixtures.length > 0)).toBe(true);
  });

  it("ships receipt extraction fixture payloads for Gmail to Sheets to email delivery", () => {
    expect(receiptExtractionToSheetsFixture.gmail).toMatchObject({
      input: { query: "from:(receipts OR orders) newer_than:30d" }
    });
    expect(receiptExtractionToSheetsFixture.sheets).toMatchObject({
      output: { appendedRows: 2 }
    });
    expect(receiptExtractionToSheetsFixture.email).toMatchObject({
      output: { channel: "email", delivered: true }
    });
  });
});

describe("mock adapter execution", () => {
  it("records versioned invocations and emits deterministic provider response ids", async () => {
    const adapter = requireMockAdapter("adapter.email.fake");
    const invocation = invocationFor({
      adapterId: "adapter.email.fake",
      operation: "email.results.send",
      payload: {
        to: "owner@example.com",
        subject: "Review",
        body: "Done",
        summary: {
          rows: 2
        }
      },
      secretRefs: {
        "email.delivery": "mock:email.delivery"
      },
      idempotencyKey: "delivery-1"
    });
    const first = await adapter.invoke(invocation);
    const second = await adapter.invoke(invocation);

    expect(first).toMatchObject({
      adapterId: "adapter.email.fake",
      operation: "email.results.send",
      operationVersion: "1.0.0",
      status: "succeeded",
      output: {
        channel: "email",
        delivered: true,
        to: "owner@example.com"
      },
      providerMetadata: {
        provider: "email",
        mock: true,
        sequence: 1
      }
    });
    expect(second.providerMetadata.providerResponseId).toBe(
      first.providerMetadata.providerResponseId
    );
    expect(adapter.invocations).toHaveLength(2);
  });

  it("creates independent mock adapter registries", async () => {
    const first = createDefaultMockAdapters();
    const second = createDefaultMockAdapters();

    await requireMockAdapter("adapter.telegram.fake", first).invoke(
      invocationFor({
        adapterId: "adapter.telegram.fake",
        operation: "telegram.alert.send",
        payload: { chatId: "ops", text: "ready", severity: "high" },
        secretRefs: { "telegram.botToken": "mock:telegram.botToken" }
      })
    );

    expect(requireMockAdapter("adapter.telegram.fake", first).invocations).toHaveLength(1);
    expect(requireMockAdapter("adapter.telegram.fake", second).invocations).toHaveLength(0);
  });

  it("fails clearly when required credential references are missing", async () => {
    const adapter = requireMockAdapter("adapter.whatsapp.fake");

    await expect(
      adapter.invoke(
        invocationFor({
          adapterId: "adapter.whatsapp.fake",
          operation: "whatsapp.alert.send",
          payload: { to: "ops", text: "urgent", severity: "high" },
          secretRefs: {}
        })
      )
    ).rejects.toThrow(AdapterCredentialError);
  });
});

describe("credential validation stubs", () => {
  it("rejects raw secret values and mock refs for real provider mode", () => {
    const email = builtinAdapterMetadata.find((adapter) => adapter.id === "adapter.email.fake");
    if (!email) {
      throw new Error("Email adapter metadata is missing.");
    }

    expect(
      validateAdapterCredentialRefs(email, {
        "email.delivery": "raw:super-secret"
      }).map((issue) => issue.code)
    ).toEqual(["ADAPTER_SECRET_RAW_VALUE"]);

    expect(
      validateAdapterCredentialRefs(
        email,
        {
          "email.delivery": "mock:email.delivery"
        },
        { requireLiveCredentials: true }
      ).map((issue) => issue.code)
    ).toEqual(["ADAPTER_REAL_CREDENTIALS_REQUIRED"]);
  });
});

function invocationFor(
  input: Omit<AdapterInvocation, "operationVersion" | "context"> &
    Partial<Pick<AdapterInvocation, "operationVersion" | "context">>
): AdapterInvocation {
  return {
    operationVersion: "1.0.0",
    context,
    ...input
  };
}
