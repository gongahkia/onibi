import { workflowSchemaVersion } from "./types.js";
import type {
  JsonRecord,
  JsonSchemaShape,
  WorkflowDeterminism,
  WorkflowRuntime,
  WorkflowSpec
} from "./types.js";

const createdAt = "2026-05-18T00:00:00.000Z";
const checksumA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const checksumB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

export const stringSchema: JsonSchemaShape = { type: "string" };
export const objectSchema: JsonSchemaShape = { type: "object", additionalProperties: true };
export const arraySchema: JsonSchemaShape = { type: "array", items: objectSchema };

const deterministicRuntime: WorkflowRuntime = {
  image: "node:20-alpine",
  command: ["node", "/workspace/run-node.js"],
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

const noExternalCalls: WorkflowDeterminism = {
  externalCalls: [],
  seededRandomness: {
    enabled: false
  },
  replayBehavior: "none"
};

function externalDeterminism(externalCalls: readonly string[]): WorkflowDeterminism {
  return {
    externalCalls,
    seededRandomness: {
      enabled: false
    },
    replayBehavior: "record"
  };
}

function workflowBase(input: {
  readonly id: string;
  readonly name: string;
  readonly prompt: string;
  readonly nodes: WorkflowSpec["nodes"];
  readonly edges: WorkflowSpec["edges"];
  readonly approval?: WorkflowSpec["approval"];
}): WorkflowSpec {
  return {
    id: input.id,
    schemaVersion: workflowSchemaVersion,
    name: input.name,
    prompt: input.prompt,
    revision: 1,
    nodes: input.nodes,
    edges: input.edges,
    approval: input.approval ?? null,
    createdAt,
    updatedAt: createdAt
  };
}

export const gmailReceiptsToSheetsWorkflowFixture = workflowBase({
  id: "workflow.gmail-receipts-to-sheets",
  name: "Gmail Receipts To Sheets",
  prompt: "Find receipt emails in Gmail and append normalized totals to a Google Sheet.",
  nodes: [
    {
      id: "manual-trigger",
      kind: "trigger",
      label: "Manual Run",
      description: "Starts the receipt sync when an operator runs the workflow.",
      inputs: {},
      outputs: {
        request: objectSchema
      },
      config: {
        trigger: "manual"
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls
    },
    {
      id: "read-gmail-receipts",
      kind: "skill",
      label: "Read Gmail Receipts",
      description: "Reads matching Gmail messages through the fake Gmail adapter.",
      inputs: {
        request: objectSchema
      },
      outputs: {
        receipts: arraySchema
      },
      config: {
        query: "from:(receipts OR orders) newer_than:30d"
      },
      runtime: deterministicRuntime,
      determinism: externalDeterminism(["adapter.gmail.fake"]),
      skillId: "skill.gmail.receipts.read"
    },
    {
      id: "normalize-receipts",
      kind: "transform",
      label: "Normalize Receipts",
      description: "Reshapes receipt messages into deterministic sheet rows.",
      inputs: {
        receipts: arraySchema
      },
      outputs: {
        rows: arraySchema
      },
      config: {
        columns: ["date", "merchant", "total", "currency"]
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls
    },
    {
      id: "append-sheet-rows",
      kind: "delivery",
      label: "Append Sheet Rows",
      description: "Writes normalized receipt rows to a fake Google Sheets adapter.",
      inputs: {
        rows: arraySchema
      },
      outputs: {
        delivery: objectSchema
      },
      config: {
        spreadsheetId: "sheet.receipts",
        range: "Receipts!A:D"
      },
      runtime: deterministicRuntime,
      determinism: externalDeterminism(["adapter.sheets.fake"]),
      adapterId: "adapter.sheets.fake"
    }
  ],
  edges: [
    {
      id: "edge.manual-trigger.read-gmail-receipts",
      source: { nodeId: "manual-trigger", port: "request" },
      target: { nodeId: "read-gmail-receipts", port: "request" }
    },
    {
      id: "edge.read-gmail-receipts.normalize-receipts",
      source: { nodeId: "read-gmail-receipts", port: "receipts" },
      target: { nodeId: "normalize-receipts", port: "receipts" }
    },
    {
      id: "edge.normalize-receipts.append-sheet-rows",
      source: { nodeId: "normalize-receipts", port: "rows" },
      target: { nodeId: "append-sheet-rows", port: "rows" }
    }
  ]
});

export const approvedGmailReceiptsToSheetsWorkflowFixture: WorkflowSpec = {
  ...gmailReceiptsToSheetsWorkflowFixture,
  approval: {
    status: "approved",
    approvedBy: "owner@example.com",
    approvedAt: "2026-05-18T01:00:00.000Z",
    frozenRevision: 1,
    frozenDagHash: checksumA,
    nodeOrder: ["manual-trigger", "read-gmail-receipts", "normalize-receipts", "append-sheet-rows"]
  }
};

export const scheduledScrapingWorkflowFixture = workflowBase({
  id: "workflow.scheduled-scraping",
  name: "Scheduled Scraping",
  prompt: "Every morning, scrape a public status page and summarize the new incidents.",
  nodes: [
    {
      id: "daily-schedule",
      kind: "trigger",
      label: "Daily Schedule",
      description: "Starts the workflow at a fixed daily wall-clock time.",
      inputs: {},
      outputs: {
        tick: objectSchema
      },
      config: {
        schedule: "0 8 * * *",
        timezone: "UTC"
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls
    },
    {
      id: "scrape-status-page",
      kind: "codegen",
      label: "Scrape Status Page",
      description: "Runs generated scraper code for a site without a registry skill.",
      inputs: {
        tick: objectSchema
      },
      outputs: {
        page: objectSchema
      },
      config: {
        url: "https://status.example.com"
      },
      runtime: deterministicRuntime,
      determinism: externalDeterminism(["https://status.example.com"]),
      codegen: {
        provenance: {
          generator: "kelpclaw.codegen.typescript",
          generatedAt: "2026-05-18T00:30:00.000Z",
          sourcePrompt: "Scrape a public status page and extract incidents.",
          artifactPath: "generated/scrape-status-page.ts",
          artifactChecksum: checksumB
        },
        replay: {
          mode: "reuse-if-unchanged",
          seed: "scheduled-scraping-v1"
        }
      }
    },
    {
      id: "summarize-incidents",
      kind: "transform",
      label: "Summarize Incidents",
      description: "Converts scraped page data into a deterministic incident summary.",
      inputs: {
        page: objectSchema
      },
      outputs: {
        summary: stringSchema
      },
      config: {
        maxItems: 5
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls
    }
  ],
  edges: [
    {
      id: "edge.daily-schedule.scrape-status-page",
      source: { nodeId: "daily-schedule", port: "tick" },
      target: { nodeId: "scrape-status-page", port: "tick" }
    },
    {
      id: "edge.scrape-status-page.summarize-incidents",
      source: { nodeId: "scrape-status-page", port: "page" },
      target: { nodeId: "summarize-incidents", port: "page" }
    }
  ]
});

export const timeSensitiveAlertDeliveryWorkflowFixture = workflowBase({
  id: "workflow.time-sensitive-alert-delivery",
  name: "Time-Sensitive Alert Delivery",
  prompt: "Monitor urgent support messages and deliver approved alerts to WhatsApp and Telegram.",
  nodes: [
    {
      id: "email-trigger",
      kind: "trigger",
      label: "Email Trigger",
      description: "Starts the workflow when urgent support email arrives.",
      inputs: {},
      outputs: {
        message: objectSchema
      },
      config: {
        source: "support@example.com"
      },
      runtime: deterministicRuntime,
      determinism: externalDeterminism(["adapter.email.fake"])
    },
    {
      id: "classify-urgency",
      kind: "skill",
      label: "Classify Urgency",
      description: "Classifies whether an incoming support message requires alert delivery.",
      inputs: {
        message: objectSchema
      },
      outputs: {
        alert: objectSchema
      },
      config: {
        threshold: "high"
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls,
      skillId: "skill.alert.urgency.classify"
    },
    {
      id: "approve-alert",
      kind: "approval",
      label: "Approve Alert",
      description: "Pauses delivery until an operator approves the alert content.",
      inputs: {
        alert: objectSchema
      },
      outputs: {
        approvedAlert: objectSchema
      },
      config: {
        requiredRole: "operator"
      },
      runtime: deterministicRuntime,
      determinism: noExternalCalls
    },
    {
      id: "send-alert",
      kind: "delivery",
      label: "Send Alert",
      description: "Sends approved alert content to fake WhatsApp and Telegram adapters.",
      inputs: {
        approvedAlert: objectSchema
      },
      outputs: {
        delivery: objectSchema
      },
      config: {
        channels: ["whatsapp", "telegram"]
      },
      runtime: deterministicRuntime,
      determinism: externalDeterminism(["adapter.whatsapp.fake", "adapter.telegram.fake"]),
      adapterId: "adapter.telegram.fake"
    }
  ],
  edges: [
    {
      id: "edge.email-trigger.classify-urgency",
      source: { nodeId: "email-trigger", port: "message" },
      target: { nodeId: "classify-urgency", port: "message" }
    },
    {
      id: "edge.classify-urgency.approve-alert",
      source: { nodeId: "classify-urgency", port: "alert" },
      target: { nodeId: "approve-alert", port: "alert" }
    },
    {
      id: "edge.approve-alert.send-alert",
      source: { nodeId: "approve-alert", port: "approvedAlert" },
      target: { nodeId: "send-alert", port: "approvedAlert" }
    }
  ]
});

export const cyclicWorkflowFixture: WorkflowSpec = {
  ...gmailReceiptsToSheetsWorkflowFixture,
  id: "workflow.cyclic",
  edges: [
    ...gmailReceiptsToSheetsWorkflowFixture.edges,
    {
      id: "edge.append-sheet-rows.read-gmail-receipts",
      source: { nodeId: "append-sheet-rows", port: "delivery" },
      target: { nodeId: "read-gmail-receipts", port: "request" }
    }
  ]
};

export const missingEdgeTargetWorkflowFixture: WorkflowSpec = {
  ...gmailReceiptsToSheetsWorkflowFixture,
  id: "workflow.missing-edge-target",
  edges: [
    {
      id: "edge.manual-trigger.missing-node",
      source: { nodeId: "manual-trigger", port: "request" },
      target: { nodeId: "missing-node", port: "request" }
    }
  ]
};

export const invalidEdgePortWorkflowFixture: WorkflowSpec = {
  ...gmailReceiptsToSheetsWorkflowFixture,
  id: "workflow.invalid-edge-port",
  edges: [
    {
      id: "edge.manual-trigger.read-gmail-receipts.invalid-port",
      source: { nodeId: "manual-trigger", port: "missing" },
      target: { nodeId: "read-gmail-receipts", port: "request" }
    }
  ]
};

export const missingCodegenMetadataWorkflowFixture: WorkflowSpec = {
  ...scheduledScrapingWorkflowFixture,
  id: "workflow.missing-codegen-metadata",
  nodes: scheduledScrapingWorkflowFixture.nodes.map((node) =>
    node.kind === "codegen" ? { ...node, codegen: undefined } : node
  )
};

export function createApprovedWorkflowFixture(
  workflow: WorkflowSpec,
  override: Partial<NonNullable<WorkflowSpec["approval"]>> = {}
): WorkflowSpec {
  return {
    ...workflow,
    approval: {
      status: "approved",
      approvedBy: "owner@example.com",
      approvedAt: "2026-05-18T01:00:00.000Z",
      frozenRevision: workflow.revision,
      frozenDagHash: checksumA,
      nodeOrder: workflow.nodes.map((node) => node.id),
      ...override
    }
  };
}

export function withConfig(
  workflow: WorkflowSpec,
  nodeId: string,
  config: JsonRecord
): WorkflowSpec {
  return {
    ...workflow,
    nodes: workflow.nodes.map((node) =>
      node.id === nodeId
        ? {
            ...node,
            config
          }
        : node
    )
  };
}
