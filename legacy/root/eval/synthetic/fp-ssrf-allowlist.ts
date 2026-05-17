import express from "express";

const app = express();

const ALLOWED_SERVICES = new Map([
  ["github", "https://api.github.com"],
  ["npm", "https://registry.npmjs.org"],
  ["pypi", "https://pypi.org/pypi"],
]);

app.get("/api-proxy", async (req, res) => {
  const service = req.query.service as string;
  if (!service) {
    res.status(400).send("missing service");
    return;
  }
  const url = ALLOWED_SERVICES.get(service); // lookup from allowlist
  if (!url) {
    res.status(403).send("unknown service");
    return;
  }
  const resp = await fetch(url); // safe: URL is from allowlist, not user-controlled
  const data = await resp.json();
  res.json(data);
});
