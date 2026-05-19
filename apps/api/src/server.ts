import { buildApiApp, createConfiguredSecretStore, createConfiguredWorkflowStore } from "./app.js";

const adminToken = process.env.KELPCLAW_ADMIN_TOKEN;
if (!adminToken) {
  throw new Error("KELPCLAW_ADMIN_TOKEN is required for the KelpClaw API server.");
}

const app = buildApiApp({
  store: createConfiguredWorkflowStore(),
  secretStore: createConfiguredSecretStore(),
  adminToken
});
const port = Number(process.env.PORT ?? 8787);

await app.listen({
  host: "0.0.0.0",
  port
});
