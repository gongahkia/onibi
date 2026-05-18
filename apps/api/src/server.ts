import { buildApiApp } from "./app.js";

const app = buildApiApp();
const port = Number(process.env.PORT ?? 8787);

await app.listen({
  host: "0.0.0.0",
  port
});
