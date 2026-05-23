import type { SkillMetadata } from "./types.js";
import type { JsonSchemaShape, WorkflowRuntime } from "@kelpclaw/workflow-spec";

const objectSchema: JsonSchemaShape = { type: "object", additionalProperties: true };
const arraySchema: JsonSchemaShape = { type: "array", items: objectSchema };

const runtimeTemplate: WorkflowRuntime = {
  image: "node:20-alpine",
  command: ["node", "/workspace/skills/run.js"],
  timeoutSeconds: 300,
  retry: {
    maxAttempts: 1,
    backoffSeconds: 0
  },
  environment: {},
  resources: {
    cpu: "1",
    memoryMb: 512
  }
};

function adapterOperation(adapterId: string, operation: string, operationVersion = "1.0.0") {
  return {
    adapterId,
    operation,
    operationVersion
  };
}

export const builtinSkills: readonly SkillMetadata[] = [
  {
    id: "skill.gmail.receipts.read",
    name: "Read Gmail Receipts",
    version: "1.0.0",
    description: "Reads receipt-like messages from Gmail and emits normalized message records.",
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities: ["gmail-receipts-read"],
    inputSchema: {
      request: objectSchema
    },
    outputSchema: {
      receipts: arraySchema
    },
    requiredSecrets: ["gmail.oauth"],
    adapterDependencies: ["adapter.gmail"],
    adapterOperations: [adapterOperation("adapter.gmail", "gmail.receipts.search")],
    runtimeTemplate,
    metaprompt:
      "Select this skill when the workflow needs Gmail receipt, order, invoice, or payment messages.",
    validationRules: ["query must be explicit", "output port must be receipts"],
    examples: [
      {
        id: "example.gmail.receipts",
        description: "Find recent receipt emails.",
        input: { request: { query: "newer_than:30d receipt" } },
        output: { receipts: [] }
      }
    ]
  },
  {
    id: "skill.sheets.rows.append",
    name: "Append Google Sheets Rows",
    version: "1.0.0",
    description: "Appends deterministic row objects to a Google Sheets range.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["sheets-rows-append"],
    inputSchema: {
      rows: arraySchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["sheets.oauth"],
    adapterDependencies: ["adapter.sheets"],
    adapterOperations: [adapterOperation("adapter.sheets", "sheets.rows.append")],
    runtimeTemplate,
    metaprompt:
      "Select this skill when structured rows should be appended to a spreadsheet or sheet range.",
    validationRules: ["range must be configured", "input port must be rows"],
    examples: [
      {
        id: "example.sheets.append",
        description: "Append receipt rows.",
        input: { rows: [{ total: 10 }] },
        output: { delivery: { status: "recorded" } }
      }
    ]
  },
  {
    id: "skill.email.results.deliver",
    name: "Deliver Email Results",
    version: "1.0.0",
    description: "Delivers workflow summaries and final result payloads through email.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["email-results-deliver"],
    inputSchema: {
      delivery: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["email.delivery"],
    adapterDependencies: ["adapter.email"],
    adapterOperations: [adapterOperation("adapter.email", "email.results.send")],
    runtimeTemplate,
    metaprompt:
      "Select this skill when a workflow needs primary result delivery, summaries, or completion notices by email.",
    validationRules: ["email is the default final result channel", "to must be configured"],
    examples: [
      {
        id: "example.email.results.deliver",
        description: "Email a workflow result summary.",
        input: { delivery: { appendedRows: 2 } },
        output: { delivery: { channel: "email", delivered: true } }
      }
    ]
  },
  {
    id: "skill.alert.urgency.classify",
    name: "Classify Alert Urgency",
    version: "1.0.0",
    description: "Classifies support messages for urgent alert delivery.",
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities: ["alert-urgency-classification"],
    inputSchema: {
      message: objectSchema
    },
    outputSchema: {
      alert: objectSchema
    },
    requiredSecrets: [],
    adapterDependencies: [],
    adapterOperations: [],
    runtimeTemplate,
    metaprompt:
      "Select this skill when a support, incident, or escalation message needs urgency classification.",
    validationRules: ["threshold must be configured", "output port must be alert"],
    examples: [
      {
        id: "example.alert.urgency",
        description: "Classify a support escalation.",
        input: { message: { subject: "urgent outage" } },
        output: { alert: { severity: "high" } }
      }
    ]
  },
  {
    id: "skill.alert.push.dispatch",
    name: "Dispatch Push Alert",
    version: "1.0.0",
    description: "Dispatches opt-in time-sensitive alerts through WhatsApp and Telegram.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["alert-push-dispatch"],
    inputSchema: {
      approvedAlert: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["whatsapp.apiKey", "telegram.botToken"],
    adapterDependencies: ["adapter.whatsapp", "adapter.telegram"],
    adapterOperations: [
      adapterOperation("adapter.whatsapp", "whatsapp.alert.send"),
      adapterOperation("adapter.telegram", "telegram.alert.send")
    ],
    runtimeTemplate,
    metaprompt:
      "Select this skill only when a workflow explicitly asks for WhatsApp or Telegram time-sensitive push alerts.",
    validationRules: [
      "WhatsApp and Telegram are opt-in secondary channels",
      "config.channels must declare each push channel"
    ],
    examples: [
      {
        id: "example.alert.push.dispatch",
        description: "Send a high severity alert over both push channels.",
        input: { approvedAlert: { severity: "high", text: "incident" } },
        output: { delivery: { channels: ["whatsapp", "telegram"], delivered: true } }
      }
    ]
  },
  {
    id: "skill.validate-workflow",
    name: "Validate Workflow",
    version: "1.0.0",
    description: "Checks a workflow spec for schema validity, stable ids, and DAG safety.",
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities: ["workflow-validation"],
    inputSchema: {
      workflow: objectSchema
    },
    outputSchema: {
      validation: objectSchema
    },
    requiredSecrets: [],
    adapterDependencies: [],
    adapterOperations: [],
    runtimeTemplate,
    metaprompt: "Select this skill when the workflow itself needs deterministic validation.",
    validationRules: ["must return stable validation codes"],
    examples: [
      {
        id: "example.workflow.validation",
        description: "Validate a workflow.",
        input: { workflow: { id: "workflow.example" } },
        output: { validation: { ok: true } }
      }
    ]
  },
  {
    id: "skill.approval.owner",
    name: "Owner Approval Gate",
    version: "1.0.0",
    description: "Blocks downstream execution until an owner approves the workflow gate.",
    deterministic: true,
    nodeKinds: ["approval"],
    capabilities: ["approval-routing"],
    inputSchema: {
      alert: objectSchema
    },
    outputSchema: {
      approvedAlert: objectSchema
    },
    requiredSecrets: [],
    adapterDependencies: [],
    adapterOperations: [],
    runtimeTemplate,
    metaprompt: "Select this skill when execution must pause for explicit human approval.",
    validationRules: ["requiredRole must be operator or owner"],
    examples: [
      {
        id: "example.approval.owner",
        description: "Approve generated copy.",
        input: { alert: { text: "review" } },
        output: { approvedAlert: { text: "review" } }
      }
    ]
  },
  {
    id: "skill.adapter.dispatch",
    name: "Adapter Dispatch",
    version: "1.0.0",
    description: "Routes a prepared payload to configured live delivery adapters.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["adapter-dispatch"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: [],
    adapterDependencies: ["adapter.email", "adapter.whatsapp", "adapter.telegram"],
    adapterOperations: [
      adapterOperation("adapter.email", "email.results.send"),
      adapterOperation("adapter.whatsapp", "whatsapp.alert.send"),
      adapterOperation("adapter.telegram", "telegram.alert.send")
    ],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow needs email, WhatsApp, or Telegram dispatch.",
    validationRules: ["delivery adapter ids must use canonical live ids"],
    examples: [
      {
        id: "example.adapter.dispatch",
        description: "Send a Telegram message.",
        input: { payload: { text: "ready" } },
        output: { delivery: { status: "recorded" } }
      }
    ]
  },
  {
    id: "skill.github.issue.create",
    name: "Create GitHub Issue",
    version: "1.0.0",
    description: "Creates a GitHub issue in a configured repository.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["github-issue-create"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["github.token"],
    adapterDependencies: ["adapter.github"],
    adapterOperations: [adapterOperation("adapter.github", "github.issue.create")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should file a GitHub issue or bug report.",
    validationRules: ["owner and repo must be configured", "title must be configured"],
    examples: [
      {
        id: "example.github.issue.create",
        description: "Open an issue for an operational alert.",
        input: { payload: { title: "Workflow alert" } },
        output: { delivery: { channel: "github", created: true } }
      }
    ]
  },
  {
    id: "skill.slack.message.send",
    name: "Send Slack Message",
    version: "1.0.0",
    description: "Sends a message to a Slack channel.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["slack-message-send"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["slack.botToken"],
    adapterDependencies: ["adapter.slack"],
    adapterOperations: [adapterOperation("adapter.slack", "slack.message.send")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow asks to notify Slack.",
    validationRules: ["channel must be configured", "text must be configured"],
    examples: [
      {
        id: "example.slack.message.send",
        description: "Post a completion notice.",
        input: { payload: { text: "Workflow completed." } },
        output: { delivery: { channel: "slack", delivered: true } }
      }
    ]
  },
  {
    id: "skill.discord.message.send",
    name: "Send Discord Message",
    version: "1.0.0",
    description: "Sends a message to a Discord channel.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["discord-message-send"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["discord.botToken"],
    adapterDependencies: ["adapter.discord"],
    adapterOperations: [adapterOperation("adapter.discord", "discord.message.send")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow asks to notify Discord.",
    validationRules: ["channelId must be configured", "content must be configured"],
    examples: [
      {
        id: "example.discord.message.send",
        description: "Post a Discord notice.",
        input: { payload: { content: "Workflow completed." } },
        output: { delivery: { channel: "discord", delivered: true } }
      }
    ]
  },
  {
    id: "skill.notion.page.create",
    name: "Create Notion Page",
    version: "1.0.0",
    description: "Creates a Notion page with configured properties.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["notion-page-create"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["notion.apiKey"],
    adapterDependencies: ["adapter.notion"],
    adapterOperations: [adapterOperation("adapter.notion", "notion.page.create")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should create a Notion page or database row.",
    validationRules: ["parent and properties must be configured"],
    examples: [
      {
        id: "example.notion.page.create",
        description: "Create a Notion task page.",
        input: { payload: { properties: { Name: "Workflow alert" } } },
        output: { delivery: { channel: "notion", created: true } }
      }
    ]
  },
  {
    id: "skill.linear.issue.create",
    name: "Create Linear Issue",
    version: "1.0.0",
    description: "Creates a Linear issue through the Linear GraphQL API.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["linear-issue-create"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["linear.apiKey"],
    adapterDependencies: ["adapter.linear"],
    adapterOperations: [adapterOperation("adapter.linear", "linear.issue.create")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should create a Linear issue.",
    validationRules: ["GraphQL query and variables must be configured"],
    examples: [
      {
        id: "example.linear.issue.create",
        description: "Create a Linear issue.",
        input: { payload: { variables: { input: { title: "Workflow alert" } } } },
        output: { delivery: { channel: "linear", created: true } }
      }
    ]
  },
  {
    id: "skill.jira.issue.create",
    name: "Create Jira Issue",
    version: "1.0.0",
    description: "Creates a Jira Cloud issue.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["jira-issue-create"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["jira.basicAuth"],
    adapterDependencies: ["adapter.jira"],
    adapterOperations: [adapterOperation("adapter.jira", "jira.issue.create")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should create a Jira ticket or issue.",
    validationRules: ["siteHost and fields must be configured"],
    examples: [
      {
        id: "example.jira.issue.create",
        description: "Create a Jira issue.",
        input: { payload: { fields: { summary: "Workflow alert" } } },
        output: { delivery: { channel: "jira", created: true } }
      }
    ]
  },
  {
    id: "skill.airtable.record.create",
    name: "Create Airtable Record",
    version: "1.0.0",
    description: "Creates an Airtable record.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["airtable-record-create"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["airtable.apiKey"],
    adapterDependencies: ["adapter.airtable"],
    adapterOperations: [adapterOperation("adapter.airtable", "airtable.record.create")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should create an Airtable record.",
    validationRules: ["baseId, tableName, and fields must be configured"],
    examples: [
      {
        id: "example.airtable.record.create",
        description: "Create an Airtable task record.",
        input: { payload: { fields: { Name: "Workflow alert" } } },
        output: { delivery: { channel: "airtable", created: true } }
      }
    ]
  },
  {
    id: "skill.webhook.post",
    name: "Post Webhook",
    version: "1.0.0",
    description: "Posts a JSON payload to a configured HTTPS webhook.",
    deterministic: true,
    nodeKinds: ["delivery"],
    capabilities: ["webhook-post"],
    inputSchema: {
      payload: objectSchema
    },
    outputSchema: {
      delivery: objectSchema
    },
    requiredSecrets: ["webhook.token"],
    adapterDependencies: ["adapter.webhook"],
    adapterOperations: [adapterOperation("adapter.webhook", "webhook.post")],
    runtimeTemplate,
    metaprompt: "Select this skill when a workflow should call a generic webhook URL.",
    validationRules: ["url and allowedHosts must be configured"],
    examples: [
      {
        id: "example.webhook.post",
        description: "Post a workflow event to a webhook.",
        input: { payload: { body: { event: "workflow.completed" } } },
        output: { delivery: { channel: "webhook", delivered: true } }
      }
    ]
  }
];
