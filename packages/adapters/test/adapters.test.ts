import { execFileSync } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AdapterCredentialError,
  OtlpExportAdapter,
  builtinAdapterMetadata,
  createDefaultLiveAdapters,
  createDefaultMockAdapters,
  createOpenApiAdapter,
  importOpenApiConnector,
  receiptExtractionToSheetsFixture,
  requireMockAdapter,
  validateAdapterCredentialRefs
} from "../src/index.js";
import type { AdapterInvocation } from "../src/index.js";
import type { DatabaseQueryInput } from "../src/index.js";

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
      "telegram",
      "github",
      "slack",
      "discord",
      "notion",
      "linear",
      "jira",
      "airtable",
      "webhook",
      "database"
    ]);
    expect(builtinAdapterMetadata.map((adapter) => adapter.id)).toEqual([
      "adapter.gmail",
      "adapter.sheets",
      "adapter.email",
      "adapter.whatsapp",
      "adapter.telegram",
      "adapter.github",
      "adapter.slack",
      "adapter.discord",
      "adapter.notion",
      "adapter.linear",
      "adapter.jira",
      "adapter.airtable",
      "adapter.webhook",
      "adapter.database"
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

  it("calls first-class HTTP SaaS adapters with auth and declared host policy", async () => {
    const calls: {
      readonly url: string;
      readonly method: string;
      readonly authorization: string | null;
      readonly body: unknown;
    }[] = [];
    const adapters = createDefaultLiveAdapters({
      fetch: async (input, init) => {
        calls.push({
          url: String(input),
          method: init?.method ?? "GET",
          authorization: new Headers(init?.headers).get("authorization"),
          body: init?.body ? JSON.parse(String(init.body)) : null
        });
        return jsonResponse({ ok: true, id: "provider-id" }, 201);
      }
    });

    const github = await adapters.get("adapter.github")?.invoke(
      invocationFor({
        adapterId: "adapter.github",
        operation: "github.issue.create",
        payload: {
          owner: "acme",
          repo: "ops",
          title: "Workflow alert",
          body: "Investigate.",
          labels: ["ops"]
        },
        secretRefs: { "github.token": "secret:github.token.default" },
        secrets: { "github.token": "github-token" }
      })
    );
    const slack = await adapters.get("adapter.slack")?.invoke(
      invocationFor({
        adapterId: "adapter.slack",
        operation: "slack.message.send",
        payload: { channel: "C123", text: "ready" },
        secretRefs: { "slack.botToken": "secret:slack.bot.default" },
        secrets: { "slack.botToken": "slack-token" }
      })
    );
    const jira = await adapters.get("adapter.jira")?.invoke(
      invocationFor({
        adapterId: "adapter.jira",
        operation: "jira.issue.create",
        payload: {
          siteHost: "acme.atlassian.net",
          fields: { summary: "Workflow alert" }
        },
        secretRefs: { "jira.basicAuth": "secret:jira.basic.default" },
        secrets: { "jira.basicAuth": "me@example.com:jira-token" }
      })
    );

    expect(github?.status).toBe("succeeded");
    expect(slack?.status).toBe("succeeded");
    expect(jira?.status).toBe("succeeded");
    expect(calls).toEqual([
      {
        url: "https://api.github.com/repos/acme/ops/issues",
        method: "POST",
        authorization: "Bearer github-token",
        body: { title: "Workflow alert", body: "Investigate.", labels: ["ops"] }
      },
      {
        url: "https://slack.com/api/chat.postMessage",
        method: "POST",
        authorization: "Bearer slack-token",
        body: { channel: "C123", text: "ready" }
      },
      {
        url: "https://acme.atlassian.net/rest/api/3/issue",
        method: "POST",
        authorization: `Basic ${Buffer.from("me@example.com:jira-token", "utf8").toString("base64")}`,
        body: { fields: { summary: "Workflow alert" } }
      }
    ]);
  });

  it("posts generic webhooks to runtime URLs", async () => {
    const calls: string[] = [];
    const adapters = createDefaultLiveAdapters({
      fetch: async (input) => {
        calls.push(String(input));
        return jsonResponse({ accepted: true });
      }
    });

    const result = await adapters.get("adapter.webhook")?.invoke(
      invocationFor({
        adapterId: "adapter.webhook",
        operation: "webhook.post",
        payload: {
          url: "https://hooks.example.test/kelpclaw",
          body: { event: "workflow.completed" }
        },
        secretRefs: { "webhook.token": "secret:webhook.token.default" },
        secrets: { "webhook.token": "webhook-token" }
      })
    );

    expect(result?.status).toBe("succeeded");
    expect(calls).toEqual(["https://hooks.example.test/kelpclaw"]);
  });

  it("passes external database engines through the runtime DatabaseClient contract", async () => {
    const calls: DatabaseQueryInput[] = [];
    const adapters = createDefaultLiveAdapters({
      database: {
        async query(input) {
          calls.push(input);
          return {
            rows: [{ id: "evt-1", status: "processed" }],
            rowCount: 1,
            fields: ["id", "status"]
          };
        }
      }
    });

    const result = await adapters.get("adapter.database")?.invoke(
      invocationFor({
        adapterId: "adapter.database",
        operation: "database.execute",
        payload: {
          statement: "INSERT INTO events (id, status) VALUES ($1, $2) RETURNING id, status",
          parameters: ["evt-1", "processed"],
          maxRows: 10
        },
        secretRefs: { "database.connection": "secret:database.connection.default" },
        secrets: {
          "database.connection": JSON.stringify({
            engine: "postgres",
            connectionString: "postgres://user:pass@db.example.test/app",
            allowWrites: true
          })
        }
      })
    );

    expect(result?.output.rows).toEqual([{ id: "evt-1", status: "processed" }]);
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      operation: "database.execute",
      statement: "INSERT INTO events (id, status) VALUES ($1, $2) RETURNING id, status",
      parameters: ["evt-1", "processed"],
      readonly: false,
      connection: {
        engine: "postgres",
        connectionString: "postgres://user:pass@db.example.test/app",
        allowWrites: true
      }
    });
  });

  it("runs SQLite database queries and writes with the built-in client", async () => {
    const databasePath = join(
      mkdtempSync(join(tmpdir(), "kelpclaw-adapter-db-")),
      "runtime.sqlite"
    );
    execFileSync("sqlite3", [databasePath], {
      input:
        "CREATE TABLE receipts (id TEXT PRIMARY KEY, total REAL);\nINSERT INTO receipts VALUES ('r1', 12.34);\n",
      encoding: "utf8"
    });
    const adapters = createDefaultLiveAdapters();

    const query = await adapters.get("adapter.database")?.invoke(
      invocationFor({
        adapterId: "adapter.database",
        operation: "database.query",
        payload: {
          statement: "SELECT id, total FROM receipts WHERE total > ?1",
          parameters: [10]
        },
        secretRefs: { "database.connection": "secret:database.connection.default" },
        secrets: {
          "database.connection": JSON.stringify({
            engine: "sqlite",
            databasePath
          })
        }
      })
    );
    const execute = await adapters.get("adapter.database")?.invoke(
      invocationFor({
        adapterId: "adapter.database",
        operation: "database.execute",
        payload: {
          statement: "INSERT INTO receipts (id, total) VALUES (?1, ?2)",
          parameters: ["r2", 99.5]
        },
        secretRefs: { "database.connection": "secret:database.connection.default" },
        secrets: {
          "database.connection": JSON.stringify({
            engine: "sqlite",
            databasePath,
            allowWrites: true
          })
        }
      })
    );

    expect(query?.output.rows).toEqual([{ id: "r1", total: 12.34 }]);
    expect(query?.output.rowCount).toBe(1);
    expect(execute?.output.rowCount).toBe(1);
  });
});

describe("OTLP export adapter", () => {
  it("exports one OTLP JSON trace with one span per tool call", async () => {
    let request: { readonly input: string; readonly body: string } | undefined;
    const adapter = new OtlpExportAdapter({
      fetch: async (input, init) => {
        request = {
          input: String(input),
          body: String(init?.body ?? "")
        };
        return jsonResponse({});
      }
    });

    const result = await adapter.invoke(
      invocationFor({
        adapterId: "adapter.otlp.export",
        operation: "otlp.traces.export",
        payload: {
          endpoint: "https://otel.test/v1/traces",
          headers: { "x-api-key": "test" },
          serviceName: "kelpclaw-test",
          runId: "agent-run.otlp",
          skillId: "skill.promoted.otlp",
          sourceAgent: "claude-code",
          promotedAt: "2026-05-23T00:00:00.000Z",
          events: [
            {
              sourceAgent: "claude-code",
              hookEvent: "PostToolUse",
              toolName: "Bash",
              toolUseId: "toolu.one",
              args: { command: "pwd" },
              result: { stdout: "/tmp" },
              status: "succeeded",
              contentHash: `sha256:${"a".repeat(64)}`,
              prevEventHash: `sha256:${"0".repeat(64)}`,
              chainIndex: 0,
              startedAt: "2026-05-23T00:00:00.000Z",
              finishedAt: "2026-05-23T00:00:01.000Z"
            },
            {
              sourceAgent: "claude-code",
              hookEvent: "PostToolUse",
              toolName: "Read",
              toolUseId: "toolu.two",
              args: { filePath: "out.txt" },
              result: { content: "ok" },
              status: "succeeded",
              contentHash: `sha256:${"b".repeat(64)}`,
              prevEventHash: `sha256:${"a".repeat(64)}`,
              chainIndex: 1,
              startedAt: "2026-05-23T00:00:02.000Z",
              finishedAt: "2026-05-23T00:00:03.000Z"
            }
          ]
        },
        secretRefs: {}
      })
    );
    const body = JSON.parse(request?.body ?? "{}");
    const spans = body.resourceSpans[0].scopeSpans[0].spans;

    expect(request?.input).toBe("https://otel.test/v1/traces");
    expect(result.status).toBe("succeeded");
    expect(result.output).toMatchObject({ accepted: true, spanCount: 2 });
    expect(spans).toHaveLength(2);
    expect(spans[1].parentSpanId).toBe(spans[0].spanId);
    expect(spans.map((span: { readonly name: string }) => span.name)).toEqual([
      "Bash PostToolUse",
      "Read PostToolUse"
    ]);
  });
});

describe("connector adapters", () => {
  it("imports OpenAPI operations and enforces declared hosts", async () => {
    const connector = await importOpenApiConnector({
      id: "connector.openapi.status",
      name: "Status API",
      document: {
        openapi: "3.1.0",
        info: { title: "Status API", version: "1.0.0" },
        servers: [{ url: "https://status.example.test" }],
        components: {
          securitySchemes: {
            bearerAuth: {
              type: "http",
              scheme: "bearer"
            }
          }
        },
        paths: {
          "/checks/{checkId}": {
            get: {
              operationId: "getCheck",
              parameters: [
                {
                  name: "checkId",
                  in: "path",
                  schema: { type: "string" }
                }
              ],
              responses: {
                "200": {
                  description: "ok",
                  content: {
                    "application/json": {
                      schema: { type: "object", properties: { status: { type: "string" } } }
                    }
                  }
                }
              }
            }
          }
        }
      }
    });
    const calls: { readonly url: string; readonly authorization: string | null }[] = [];
    const adapter = createOpenApiAdapter(connector, {
      fetch: async (input, init) => {
        const headers = new Headers(init?.headers);
        calls.push({
          url: String(input),
          authorization: headers.get("authorization")
        });
        return jsonResponse({ status: "ok" });
      }
    });

    const result = await adapter.invoke(
      invocationFor({
        adapterId: connector.adapterId,
        operation: "getCheck",
        payload: {
          path: { checkId: "api" },
          query: { verbose: true }
        },
        secrets: {
          bearerAuth: "status-token"
        },
        secretRefs: {
          bearerAuth: "secret:status-token"
        }
      })
    );

    expect(connector.operations[0]).toMatchObject({
      name: "getCheck",
      method: "GET",
      path: "/checks/{checkId}"
    });
    expect(result.status).toBe("succeeded");
    expect(calls).toEqual([
      {
        url: "https://status.example.test/checks/api?verbose=true",
        authorization: "Bearer status-token"
      }
    ]);
    await expect(
      createOpenApiAdapter({
        ...connector,
        operations: [
          {
            ...connector.operations[0]!,
            metadata: {
              ...(connector.operations[0]!.metadata ?? {}),
              url: "https://evil.example.test/checks/api"
            }
          }
        ]
      }).invoke(
        invocationFor({
          adapterId: connector.adapterId,
          operation: "getCheck",
          payload: {},
          secrets: { bearerAuth: "status-token" },
          secretRefs: { bearerAuth: "secret:status-token" }
        })
      )
    ).rejects.toThrow("not declared");
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
