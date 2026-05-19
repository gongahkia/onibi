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
    adapterDependencies: ["adapter.gmail.fake"],
    adapterOperations: [adapterOperation("adapter.gmail.fake", "gmail.receipts.search")],
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
    adapterDependencies: ["adapter.sheets.fake"],
    adapterOperations: [adapterOperation("adapter.sheets.fake", "sheets.rows.append")],
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
    adapterDependencies: ["adapter.email.fake"],
    adapterOperations: [adapterOperation("adapter.email.fake", "email.results.send")],
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
    adapterDependencies: ["adapter.whatsapp.fake", "adapter.telegram.fake"],
    adapterOperations: [
      adapterOperation("adapter.whatsapp.fake", "whatsapp.alert.send"),
      adapterOperation("adapter.telegram.fake", "telegram.alert.send")
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
    description: "Routes a prepared payload to configured fake adapters in local test mode.",
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
    adapterDependencies: ["adapter.email.fake", "adapter.whatsapp.fake", "adapter.telegram.fake"],
    adapterOperations: [
      adapterOperation("adapter.email.fake", "email.results.send"),
      adapterOperation("adapter.whatsapp.fake", "whatsapp.alert.send"),
      adapterOperation("adapter.telegram.fake", "telegram.alert.send")
    ],
    runtimeTemplate,
    metaprompt:
      "Select this skill when a workflow needs fake email, WhatsApp, or Telegram dispatch.",
    validationRules: ["only fake adapters are allowed in Phase 2"],
    examples: [
      {
        id: "example.adapter.dispatch",
        description: "Send a fake Telegram message.",
        input: { payload: { text: "ready" } },
        output: { delivery: { status: "recorded" } }
      }
    ]
  }
];
