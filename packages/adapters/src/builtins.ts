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
const booleanSchema = { type: "boolean" } as const;

const defaultRateLimit = {
  maxRequests: 60,
  perSeconds: 60
} as const;

const defaultRetry = {
  maxAttempts: 3,
  backoffSeconds: 2,
  retryableErrorCodes: ["RATE_LIMITED", "TEMPORARY_UNAVAILABLE"]
} as const;

const noneNetworkPolicy = {
  mode: "none",
  allowedHosts: []
} as const;

const gmailNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["oauth2.googleapis.com", "gmail.googleapis.com"]
} as const;

const sheetsNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["oauth2.googleapis.com", "sheets.googleapis.com"]
} as const;

const smtpNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["smtp"]
} as const;

const whatsappNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["graph.facebook.com"]
} as const;

const telegramNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["api.telegram.org"]
} as const;

const githubNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["api.github.com"]
} as const;

const slackNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["slack.com"]
} as const;

const discordNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["discord.com"]
} as const;

const notionNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["api.notion.com"]
} as const;

const linearNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["api.linear.app"]
} as const;

const jiraNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["*.atlassian.net"]
} as const;

const airtableNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["api.airtable.com"]
} as const;

const webhookNetworkPolicy = {
  mode: "declared",
  allowedHosts: ["*"]
} as const;

const gmailSecret = secret("gmail.oauth", "OAuth token reference for Gmail scopes.");
const sheetsSecret = secret("sheets.oauth", "OAuth token reference for Google Sheets scopes.");
const emailSecret = secret("email.delivery", "Provider key or SMTP credential reference.");
const whatsappSecret = secret("whatsapp.apiKey", "WhatsApp Business API key reference.");
const telegramSecret = secret("telegram.botToken", "Telegram bot token reference.");
const githubSecret = secret("github.token", "GitHub fine-grained or classic token reference.");
const slackSecret = secret("slack.botToken", "Slack bot token reference.");
const discordSecret = secret("discord.botToken", "Discord bot token reference.");
const notionSecret = secret("notion.apiKey", "Notion integration token reference.");
const linearSecret = secret("linear.apiKey", "Linear API key reference.");
const jiraSecret = secret("jira.basicAuth", "Jira email:api-token credential reference.");
const airtableSecret = secret("airtable.apiKey", "Airtable personal access token reference.");
const webhookSecret = secret("webhook.token", "Generic webhook bearer token reference.");

export const builtinAdapterMetadata = [
  adapter({
    id: "adapter.gmail",
    kind: "gmail",
    displayName: "Gmail",
    networkPolicy: gmailNetworkPolicy,
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
    id: "adapter.sheets",
    kind: "sheets",
    displayName: "Google Sheets",
    networkPolicy: sheetsNetworkPolicy,
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
    id: "adapter.email",
    kind: "email",
    displayName: "SMTP Email Delivery",
    networkPolicy: smtpNetworkPolicy,
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
    id: "adapter.whatsapp",
    kind: "whatsapp",
    displayName: "WhatsApp Cloud Alerts",
    networkPolicy: whatsappNetworkPolicy,
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
    id: "adapter.telegram",
    kind: "telegram",
    displayName: "Telegram Alerts",
    networkPolicy: telegramNetworkPolicy,
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
  }),
  adapter({
    id: "adapter.github",
    kind: "github",
    displayName: "GitHub",
    networkPolicy: githubNetworkPolicy,
    capabilities: ["github.issue.create", "github.issue.comment"],
    requiredSecrets: [githubSecret],
    operations: [
      operation(
        "github.issue.create",
        "Creates a GitHub issue in a repository.",
        {
          owner: stringSchema,
          repo: stringSchema,
          title: stringSchema,
          body: stringSchema,
          labels: arraySchema
        },
        { response: objectSchema }
      ),
      operation(
        "github.issue.comment",
        "Adds a comment to an existing GitHub issue.",
        {
          owner: stringSchema,
          repo: stringSchema,
          issueNumber: { type: "integer" },
          body: stringSchema
        },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.github.issue.create",
        "Create a GitHub issue fixture.",
        "github.issue.create",
        { owner: "acme", repo: "ops", title: "Workflow alert", body: "Investigate." },
        { response: { status: 201, body: { number: 42 } } }
      )
    ]
  }),
  adapter({
    id: "adapter.slack",
    kind: "slack",
    displayName: "Slack",
    networkPolicy: slackNetworkPolicy,
    capabilities: ["slack.message.send"],
    requiredSecrets: [slackSecret],
    operations: [
      operation(
        "slack.message.send",
        "Sends a Slack message with a bot token.",
        { channel: stringSchema, text: stringSchema, blocks: arraySchema },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.slack.message.send",
        "Send a Slack message fixture.",
        "slack.message.send",
        { channel: "C123", text: "Workflow completed." },
        { response: { status: 200, body: { ok: true } } }
      )
    ]
  }),
  adapter({
    id: "adapter.discord",
    kind: "discord",
    displayName: "Discord",
    networkPolicy: discordNetworkPolicy,
    capabilities: ["discord.message.send"],
    requiredSecrets: [discordSecret],
    operations: [
      operation(
        "discord.message.send",
        "Sends a Discord channel message with a bot token.",
        { channelId: stringSchema, content: stringSchema, embeds: arraySchema },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.discord.message.send",
        "Send a Discord message fixture.",
        "discord.message.send",
        { channelId: "123", content: "Workflow completed." },
        { response: { status: 200, body: { id: "message-1" } } }
      )
    ]
  }),
  adapter({
    id: "adapter.notion",
    kind: "notion",
    displayName: "Notion",
    networkPolicy: notionNetworkPolicy,
    capabilities: ["notion.page.create"],
    requiredSecrets: [notionSecret],
    operations: [
      operation(
        "notion.page.create",
        "Creates a Notion page.",
        { parent: objectSchema, properties: objectSchema, children: arraySchema },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.notion.page.create",
        "Create a Notion page fixture.",
        "notion.page.create",
        { parent: { database_id: "db" }, properties: { Name: { title: [] } } },
        { response: { status: 200, body: { id: "page-1" } } }
      )
    ]
  }),
  adapter({
    id: "adapter.linear",
    kind: "linear",
    displayName: "Linear",
    networkPolicy: linearNetworkPolicy,
    capabilities: ["linear.issue.create"],
    requiredSecrets: [linearSecret],
    operations: [
      operation(
        "linear.issue.create",
        "Creates a Linear issue through GraphQL.",
        { query: stringSchema, variables: objectSchema },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.linear.issue.create",
        "Create a Linear issue fixture.",
        "linear.issue.create",
        {
          query:
            "mutation IssueCreate($input: IssueCreateInput!) { issueCreate(input: $input) { success } }"
        },
        { response: { status: 200, body: { data: { issueCreate: { success: true } } } } }
      )
    ]
  }),
  adapter({
    id: "adapter.jira",
    kind: "jira",
    displayName: "Jira Cloud",
    networkPolicy: jiraNetworkPolicy,
    capabilities: ["jira.issue.create"],
    requiredSecrets: [jiraSecret],
    operations: [
      operation(
        "jira.issue.create",
        "Creates a Jira Cloud issue.",
        {
          siteHost: stringSchema,
          fields: objectSchema
        },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.jira.issue.create",
        "Create a Jira issue fixture.",
        "jira.issue.create",
        { path: { siteHost: "acme.atlassian.net" }, fields: { summary: "Workflow alert" } },
        { response: { status: 201, body: { key: "OPS-42" } } }
      )
    ]
  }),
  adapter({
    id: "adapter.airtable",
    kind: "airtable",
    displayName: "Airtable",
    networkPolicy: airtableNetworkPolicy,
    capabilities: ["airtable.record.create"],
    requiredSecrets: [airtableSecret],
    operations: [
      operation(
        "airtable.record.create",
        "Creates an Airtable record.",
        {
          baseId: stringSchema,
          tableName: stringSchema,
          fields: objectSchema,
          typecast: booleanSchema
        },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.airtable.record.create",
        "Create an Airtable record fixture.",
        "airtable.record.create",
        { path: { baseId: "app123", tableName: "Tasks" }, fields: { Name: "Workflow alert" } },
        { response: { status: 200, body: { id: "rec123" } } }
      )
    ]
  }),
  adapter({
    id: "adapter.webhook",
    kind: "webhook",
    displayName: "Generic Webhook",
    networkPolicy: webhookNetworkPolicy,
    capabilities: ["webhook.post"],
    requiredSecrets: [webhookSecret],
    operations: [
      operation(
        "webhook.post",
        "Posts a JSON payload to a runtime-configured HTTPS webhook URL.",
        { url: stringSchema, body: objectSchema, headers: objectSchema, allowedHosts: arraySchema },
        { response: objectSchema }
      )
    ],
    fixtures: [
      fixture(
        "fixture.webhook.post",
        "Post a generic webhook fixture.",
        "webhook.post",
        {
          url: "https://hooks.example.test/kelpclaw",
          body: { ok: true },
          allowedHosts: ["hooks.example.test"]
        },
        { response: { status: 200, body: { accepted: true } } }
      )
    ]
  })
] as const satisfies readonly AdapterMetadata[];

export const mockAdapterMetadata = builtinAdapterMetadata.map((metadata) => ({
  ...metadata,
  id: `${metadata.id}.fake`,
  displayName: `Mock ${metadata.displayName}`,
  networkPolicy: noneNetworkPolicy,
  live: false
})) as readonly AdapterMetadata[];
export const fakeAdapterMetadata = mockAdapterMetadata;

export function createDefaultMockAdapters(): Map<string, MockAdapter> {
  return new Map<string, MockAdapter>(
    [...builtinAdapterMetadata, ...mockAdapterMetadata].map((metadata) => [
      metadata.id,
      createMockAdapter(metadata)
    ])
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
  readonly networkPolicy: AdapterMetadata["networkPolicy"];
}): AdapterMetadata {
  return {
    ...input,
    version: "1.0.0",
    rateLimit: defaultRateLimit,
    retry: defaultRetry,
    live: true
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
