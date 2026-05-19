import { describe, expect, it } from "vitest";
import {
  AdapterCredentialError,
  builtinAdapterMetadata,
  createDefaultLiveAdapters,
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
    expect(builtinAdapterMetadata.map((adapter) => adapter.id)).toEqual([
      "adapter.gmail",
      "adapter.sheets",
      "adapter.email",
      "adapter.whatsapp",
      "adapter.telegram"
    ]);
    expect(builtinAdapterMetadata.every((adapter) => adapter.live)).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.version === "1.0.0")).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.operations.length > 0)).toBe(true);
    expect(builtinAdapterMetadata.every((adapter) => adapter.requiredSecrets.length > 0)).toBe(
      true
    );
    expect(
      builtinAdapterMetadata.every((adapter) => adapter.networkPolicy.mode === "declared")
    ).toBe(true);
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

  it("redacts stored adapter payload echoes and secret references", async () => {
    const adapter = requireMockAdapter("adapter.email.fake");

    await adapter.invoke(
      invocationFor({
        adapterId: "adapter.email.fake",
        operation: "email.results.send",
        payload: {
          to: "owner@example.com",
          subject: "Review",
          body: "Done",
          authorization: "Bearer provider-token"
        },
        secretRefs: {
          "email.delivery": "mock:email.delivery"
        }
      })
    );

    expect(adapter.invocations[0]?.payload.authorization).toBe("[REDACTED]");
    expect(adapter.invocations[0]?.secretRefs["email.delivery"]).toBe("[REDACTED]");
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

describe("live adapter execution", () => {
  it("calls Gmail and Sheets HTTP APIs with resolved Google OAuth secrets", async () => {
    const calls: string[] = [];
    const adapters = createDefaultLiveAdapters({
      googleApiBaseUrl: "https://google.test",
      googleTokenUrl: "https://oauth.test/token",
      fetch: async (input) => {
        const url = String(input);
        calls.push(url);
        if (url === "https://oauth.test/token") {
          return jsonResponse({ access_token: "google-access-token" });
        }
        if (url.startsWith("https://google.test/gmail/v1/users/me/messages?")) {
          return jsonResponse({
            messages: [{ id: "msg-1", threadId: "thread-1" }],
            resultSizeEstimate: 1
          });
        }
        if (url.includes("/gmail/v1/users/me/messages/msg-1")) {
          return jsonResponse({
            id: "msg-1",
            threadId: "thread-1",
            internalDate: "1770000000000",
            snippet: "Total USD 12.34",
            payload: {
              headers: [{ name: "Subject", value: "Receipt from Tidepool Market" }]
            }
          });
        }
        if (url.includes("/v4/spreadsheets/sheet.receipts/values/Receipts")) {
          return jsonResponse({
            updates: {
              updatedRange: "Receipts!A1:D1",
              updatedRows: 1
            }
          });
        }

        return jsonResponse({ error: "unexpected" }, 500);
      }
    });

    const gmail = await adapters.get("adapter.gmail")?.invoke(
      invocationFor({
        adapterId: "adapter.gmail",
        operation: "gmail.receipts.search",
        payload: { query: "receipt", maxResults: 1 },
        secretRefs: { "gmail.oauth": "secret:google.oauth.default" },
        secrets: {
          "gmail.oauth": JSON.stringify({
            refreshToken: "refresh",
            clientId: "client",
            clientSecret: "secret"
          })
        }
      })
    );
    const sheets = await adapters.get("adapter.sheets")?.invoke(
      invocationFor({
        adapterId: "adapter.sheets",
        operation: "sheets.rows.append",
        payload: {
          spreadsheetId: "sheet.receipts",
          range: "Receipts!A:D",
          rows: [{ date: "2026-05-18", total: 12.34 }],
          columns: ["date", "total"]
        },
        secretRefs: { "sheets.oauth": "secret:google.oauth.default" },
        secrets: {
          "sheets.oauth": JSON.stringify({
            accessToken: "google-access-token"
          })
        }
      })
    );

    expect(gmail?.providerMetadata.mock).toBe(false);
    expect(gmail?.output.receipts).toEqual([
      expect.objectContaining({ messageId: "msg-1", total: 12.34, currency: "USD" })
    ]);
    expect(sheets?.output.appendedRows).toBe(1);
    expect(calls.some((call) => call.includes("oauth.test/token"))).toBe(true);
  });

  it("calls WhatsApp and Telegram live HTTP APIs with resolved secrets", async () => {
    const calls: string[] = [];
    const adapters = createDefaultLiveAdapters({
      whatsappApiBaseUrl: "https://graph.test",
      telegramApiBaseUrl: "https://telegram.test",
      fetch: async (input) => {
        const url = String(input);
        calls.push(url);
        if (url.includes("/messages")) {
          return jsonResponse({ messages: [{ id: "wamid.1" }] });
        }
        if (url.includes("/sendMessage")) {
          return jsonResponse({ ok: true, result: { message_id: 42 } });
        }
        return jsonResponse({ error: "unexpected" }, 500);
      }
    });

    const whatsapp = await adapters.get("adapter.whatsapp")?.invoke(
      invocationFor({
        adapterId: "adapter.whatsapp",
        operation: "whatsapp.alert.send",
        payload: { to: "15551234567", text: "urgent", phoneNumberId: "phone-1" },
        secretRefs: { "whatsapp.apiKey": "secret:whatsapp.cloud.default" },
        secrets: { "whatsapp.apiKey": "whatsapp-token" }
      })
    );
    const telegram = await adapters.get("adapter.telegram")?.invoke(
      invocationFor({
        adapterId: "adapter.telegram",
        operation: "telegram.alert.send",
        payload: { chatId: "ops", text: "urgent" },
        secretRefs: { "telegram.botToken": "secret:telegram.bot.default" },
        secrets: { "telegram.botToken": "telegram-token" }
      })
    );

    expect(whatsapp?.output.messageId).toBe("wamid.1");
    expect(telegram?.output.messageId).toBe("42");
    expect(calls).toEqual([
      "https://graph.test/v20.0/phone-1/messages",
      "https://telegram.test/bottelegram-token/sendMessage"
    ]);
  });
});

describe("credential validation", () => {
  it("rejects raw secret values and mock refs for real provider mode", () => {
    const email = builtinAdapterMetadata.find((adapter) => adapter.id === "adapter.email");
    if (!email) {
      throw new Error("Email adapter metadata is missing.");
    }

    expect(
      validateAdapterCredentialRefs(email, {
        "email.delivery": "raw:super-secret"
      }).map((issue) => issue.code)
    ).toEqual(["ADAPTER_SECRET_RAW_VALUE"]);

    expect(
      validateAdapterCredentialRefs(email, {
        "email.delivery": "mock:email.delivery"
      }).map((issue) => issue.code)
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

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json"
    }
  });
}
