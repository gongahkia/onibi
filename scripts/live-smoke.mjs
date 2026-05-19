import {
  gmailReceiptsToSheetsWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture
} from "../packages/workflow-spec/dist/index.js";

const apiBaseUrl = process.env.KELPCLAW_API_BASE_URL ?? "http://127.0.0.1:8787";
const adminToken = process.env.KELPCLAW_ADMIN_TOKEN ?? "";

if (process.env.KELPCLAW_LIVE_SMOKE !== "1") {
  console.log("Skipping live smoke. Set KELPCLAW_LIVE_SMOKE=1 to run real provider calls.");
  process.exit(0);
}

const requiredEnv = [
  "KELPCLAW_ADMIN_TOKEN",
  "KELPCLAW_SMOKE_SHEET_ID",
  "KELPCLAW_SMOKE_EMAIL_TO",
  "KELPCLAW_SMOKE_WHATSAPP_TO",
  "KELPCLAW_SMOKE_TELEGRAM_CHAT_ID"
];
const missingEnv = requiredEnv.filter((name) => !process.env[name]);
if (missingEnv.length > 0) {
  console.error(`Missing live smoke environment: ${missingEnv.join(", ")}`);
  process.exit(1);
}

await request("GET", "/health", undefined, { auth: false });
const secrets = await request("GET", "/api/secrets");
const blocked = secrets.integrations
  .filter((integration) => !integration.ready)
  .map((integration) => integration.id);
if (blocked.length > 0) {
  throw new Error(`Live smoke blocked by missing integration secrets: ${blocked.join(", ")}`);
}

await runWorkflow(smokeReceiptWorkflow());
await runWorkflow(smokePushWorkflow());

console.log("Live provider smoke completed.");

async function runWorkflow(workflow) {
  const created = await request("POST", "/api/workflows", workflow);
  const approved = await request("POST", `/api/workflows/${workflow.id}/approve`, {
    workflow: created.workflow,
    approvedBy: "live-smoke@kelpclaw.local"
  });
  const run = await request("POST", `/api/workflows/${workflow.id}/runs`, {
    approvedRevisionId: approved.approvedRevisionId
  });
  if (run.run.status !== "succeeded") {
    console.error(JSON.stringify(run.run.events, null, 2));
    throw new Error(`Workflow '${workflow.id}' live smoke failed with status '${run.run.status}'.`);
  }
}

function smokeReceiptWorkflow() {
  const id = `workflow.live-smoke.receipts.${Date.now()}`;
  return {
    ...gmailReceiptsToSheetsWorkflowFixture,
    id,
    name: "Live Smoke Receipts",
    prompt: "Live smoke Gmail receipts into a Google Sheet and SMTP email.",
    approval: null,
    updatedAt: new Date().toISOString(),
    nodes: gmailReceiptsToSheetsWorkflowFixture.nodes.map((node) => {
      if (node.id === "read-gmail-receipts") {
        return {
          ...node,
          config: {
            ...node.config,
            query: process.env.KELPCLAW_SMOKE_GMAIL_QUERY ?? "newer_than:7d"
          }
        };
      }
      if (node.id === "append-sheet-rows") {
        return {
          ...node,
          config: {
            ...node.config,
            spreadsheetId: process.env.KELPCLAW_SMOKE_SHEET_ID
          }
        };
      }
      if (node.id === "deliver-results-email") {
        return {
          ...node,
          config: {
            ...node.config,
            to: process.env.KELPCLAW_SMOKE_EMAIL_TO
          }
        };
      }

      return node;
    })
  };
}

function smokePushWorkflow() {
  const id = `workflow.live-smoke.push.${Date.now()}`;
  const trigger = {
    ...timeSensitiveAlertDeliveryWorkflowFixture.nodes[0],
    adapterId: undefined,
    adapterIds: undefined,
    adapterOperations: undefined,
    secretRefs: undefined,
    determinism: {
      externalCalls: [],
      seededRandomness: { enabled: false },
      replayBehavior: "none"
    },
    config: {
      source: "live-smoke"
    }
  };

  return {
    ...timeSensitiveAlertDeliveryWorkflowFixture,
    id,
    name: "Live Smoke Push Alerts",
    prompt: "Live smoke WhatsApp and Telegram alert delivery.",
    approval: null,
    updatedAt: new Date().toISOString(),
    nodes: timeSensitiveAlertDeliveryWorkflowFixture.nodes.map((node) => {
      if (node.id === trigger.id) {
        return trigger;
      }
      if (node.id === "send-alert") {
        return {
          ...node,
          config: {
            ...node.config,
            to: process.env.KELPCLAW_SMOKE_WHATSAPP_TO,
            chatId: process.env.KELPCLAW_SMOKE_TELEGRAM_CHAT_ID,
            text: process.env.KELPCLAW_SMOKE_ALERT_TEXT ?? "KelpClaw live smoke alert"
          }
        };
      }

      return node;
    })
  };
}

async function request(method, path, body, options = { auth: true }) {
  const response = await fetch(new URL(path, apiBaseUrl), {
    method,
    headers: {
      ...(options.auth ? { authorization: `Bearer ${adminToken}` } : {}),
      ...(body === undefined ? {} : { "content-type": "application/json" })
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  const text = await response.text();
  const payload = text.length > 0 ? JSON.parse(text) : {};
  if (!response.ok) {
    throw new Error(
      `${method} ${path} failed with ${response.status}: ${payload.message ?? payload.error ?? text}`
    );
  }

  return payload;
}
