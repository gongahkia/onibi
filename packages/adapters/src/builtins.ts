import { createMockAdapter } from "./mock-adapter.js";
import {
  emailResultDeliveryFixture,
  gmailReceiptPayloadFixture,
  gmailReceiptSearchInputFixture,
  sheetsReceiptRowsFixture
} from "./fixtures.js";
import type { MockAdapter } from "./mock-adapter.js";
import type {
  AdapterFixturePayload,
  AdapterMetadata,
  AdapterOperationDefinition,
  AdapterSecretRequirement
} from "./types.js";

const objectSchema = { type: "object", additionalProperties: true } as const;
const arraySchema = { type: "array", items: objectSchema } as const;
const stringSchema = { type: "string" } as const;

const defaultRateLimit = {
  maxRequests: 60,
  perSeconds: 60
} as const;

const defaultRetry = {
  maxAttempts: 3,
  backoffSeconds: 2,
  retryableErrorCodes: ["RATE_LIMITED", "TEMPORARY_UNAVAILABLE"]
} as const;

const mockNetworkPolicy = {
  mode: "none",
  allowedHosts: []
} as const;

const gmailSecret = secret("gmail.oauth", "OAuth token reference for Gmail scopes.");
const sheetsSecret = secret("sheets.oauth", "OAuth token reference for Google Sheets scopes.");
const emailSecret = secret("email.delivery", "Provider key or SMTP credential reference.");
const whatsappSecret = secret("whatsapp.apiKey", "WhatsApp Business API key reference.");
const telegramSecret = secret("telegram.botToken", "Telegram bot token reference.");

export const builtinAdapterMetadata = [
  adapter({
    id: "adapter.gmail.fake",
    kind: "gmail",
    displayName: "Mock Gmail",
    capabilities: ["gmail.trigger", "gmail.receipts.search"],
    requiredSecrets: [gmailSecret],
    operations: [
      operation(
        "gmail.trigger.poll",
        "Polls Gmail for messages that can trigger a workflow.",
        { request: objectSchema },
        { messages: arraySchema }
      ),
      operation(
        "gmail.receipts.search",
        "Searches Gmail for receipt-like messages and emits normalized receipt records.",
        { query: stringSchema, maxResults: { type: "integer" } },
        { receipts: arraySchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.gmail.receipts.search",
        "Receipt search fixture used for Gmail to Sheets contract tests.",
        "gmail.receipts.search",
        gmailReceiptSearchInputFixture,
        gmailReceiptPayloadFixture
      )
    ]
  }),
  adapter({
    id: "adapter.sheets.fake",
    kind: "sheets",
    displayName: "Mock Google Sheets",
    capabilities: ["sheets.rows.append", "sheets.rows.update", "sheets.rows.lookup"],
    requiredSecrets: [sheetsSecret],
    operations: [
      operation(
        "sheets.rows.append",
        "Appends row objects to a Google Sheets range.",
        { spreadsheetId: stringSchema, range: stringSchema, rows: arraySchema },
        { spreadsheetId: stringSchema, range: stringSchema, appendedRows: { type: "integer" } }
      ),
      operation(
        "sheets.rows.update",
        "Updates matching row objects in a Google Sheets range.",
        { spreadsheetId: stringSchema, range: stringSchema, rows: arraySchema },
        { updatedRows: { type: "integer" } }
      ),
      operation(
        "sheets.rows.lookup",
        "Looks up row objects from a Google Sheets range.",
        { spreadsheetId: stringSchema, range: stringSchema, lookup: objectSchema },
        { rows: arraySchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.sheets.receipts.append",
        "Rows transformed from receipt payloads for append contract tests.",
        "sheets.rows.append",
        sheetsReceiptRowsFixture,
        {
          spreadsheetId: "sheet.receipts",
          range: "Receipts!A:D",
          appendedRows: 2
        }
      )
    ]
  }),
  adapter({
    id: "adapter.email.fake",
    kind: "email",
    displayName: "Mock Email Delivery",
    capabilities: ["email.approval.request", "email.results.send"],
    requiredSecrets: [emailSecret],
    operations: [
      operation(
        "email.approval.request",
        "Sends an email approval request for a workflow gate.",
        { to: stringSchema, subject: stringSchema, body: stringSchema },
        { approvalRequestId: stringSchema, channel: stringSchema, delivered: { type: "boolean" } }
      ),
      operation(
        "email.results.send",
        "Delivers workflow summaries and final result payloads by email.",
        { to: stringSchema, subject: stringSchema, body: stringSchema, summary: objectSchema },
        { messageId: stringSchema, channel: stringSchema, delivered: { type: "boolean" } }
      )
    ],
    fixtures: [
      fixture(
        "fixture.email.results.send",
        "Default final result delivery fixture.",
        "email.results.send",
        emailResultDeliveryFixture,
        {
          delivered: true,
          channel: "email"
        }
      )
    ]
  }),
  adapter({
    id: "adapter.whatsapp.fake",
    kind: "whatsapp",
    displayName: "Mock WhatsApp Alerts",
    capabilities: ["whatsapp.alert.send"],
    requiredSecrets: [whatsappSecret],
    operations: [
      operation(
        "whatsapp.alert.send",
        "Sends opt-in time-sensitive workflow alerts over WhatsApp.",
        { to: stringSchema, text: stringSchema, severity: stringSchema },
        { messageId: stringSchema, channel: stringSchema, delivered: { type: "boolean" } }
      )
    ],
    fixtures: [
      fixture(
        "fixture.whatsapp.alert.send",
        "Opt-in WhatsApp alert fixture.",
        "whatsapp.alert.send",
        { to: "ops-whatsapp", text: "Urgent incident", severity: "high" },
        { delivered: true, channel: "whatsapp" }
      )
    ]
  }),
  adapter({
    id: "adapter.telegram.fake",
    kind: "telegram",
    displayName: "Mock Telegram Alerts",
    capabilities: ["telegram.alert.send"],
    requiredSecrets: [telegramSecret],
    operations: [
      operation(
        "telegram.alert.send",
        "Sends opt-in time-sensitive workflow alerts over Telegram.",
        { chatId: stringSchema, text: stringSchema, severity: stringSchema },
        { messageId: stringSchema, channel: stringSchema, delivered: { type: "boolean" } }
      )
    ],
    fixtures: [
      fixture(
        "fixture.telegram.alert.send",
        "Opt-in Telegram alert fixture.",
        "telegram.alert.send",
        { chatId: "ops-telegram", text: "Urgent incident", severity: "high" },
        { delivered: true, channel: "telegram" }
      )
    ]
  })
] as const satisfies readonly AdapterMetadata[];

export const fakeAdapterMetadata = builtinAdapterMetadata;

export function createDefaultMockAdapters(): Map<string, MockAdapter> {
  return new Map<string, MockAdapter>(
    builtinAdapterMetadata.map((metadata) => [metadata.id, createMockAdapter(metadata)])
  );
}

export const createDefaultFakeAdapters = createDefaultMockAdapters;

export function requireMockAdapter(adapterId: string, adapters = createDefaultMockAdapters()) {
  const adapter = adapters.get(adapterId);
  if (!adapter) {
    throw new Error(`Unknown mock adapter '${adapterId}'.`);
  }

  return adapter;
}

export const requireFakeAdapter = requireMockAdapter;

function adapter(input: {
  readonly id: AdapterMetadata["id"];
  readonly kind: AdapterMetadata["kind"];
  readonly displayName: AdapterMetadata["displayName"];
  readonly capabilities: AdapterMetadata["capabilities"];
  readonly operations: AdapterMetadata["operations"];
  readonly requiredSecrets: AdapterMetadata["requiredSecrets"];
  readonly fixtures: AdapterMetadata["fixtures"];
}): AdapterMetadata {
  return {
    ...input,
    version: "1.0.0",
    networkPolicy: mockNetworkPolicy,
    rateLimit: defaultRateLimit,
    retry: defaultRetry,
    live: false
  };
}

function operation(
  name: string,
  description: string,
  inputSchema: AdapterOperationDefinition["inputSchema"],
  outputSchema: AdapterOperationDefinition["outputSchema"]
): AdapterOperationDefinition {
  return {
    name,
    version: "1.0.0",
    description,
    inputSchema,
    outputSchema
  };
}

function secret(name: string, description: string): AdapterSecretRequirement {
  return {
    name,
    description,
    mockRef: `mock:${name}`
  };
}

function fixture(
  id: string,
  description: string,
  operationName: string,
  input: AdapterFixturePayload["input"],
  output: AdapterFixturePayload["output"]
): AdapterFixturePayload {
  return {
    id,
    description,
    operation: operationName,
    input,
    output
  };
}
