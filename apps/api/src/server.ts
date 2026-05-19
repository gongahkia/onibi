import { buildApiApp, createConfiguredWorkflowStore } from "./app.js";

const app = buildApiApp({
  store: createConfiguredWorkflowStore()
});
const port = Number(process.env.PORT ?? 8787);

await app.listen({
  host: "0.0.0.0",
  port
});
