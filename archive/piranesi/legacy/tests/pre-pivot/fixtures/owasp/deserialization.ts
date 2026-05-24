import express from "express";
import yaml from "js-yaml";

const app = express();

// VULNERABLE: JSON.parse on raw user input without schema validation
app.post("/api/config", (req, res) => {
  const rawBody = req.body.config; // tainted
  const parsed = JSON.parse(rawBody); // CWE-502 sink
  applyConfig(parsed);
  res.json({ ok: true });
});

// VULNERABLE: yaml.load on user input
app.post("/api/import", (req, res) => {
  const yamlData = req.body.data; // tainted
  const parsed = yaml.load(yamlData); // CWE-502 sink (unsafe loader)
  res.json(parsed);
});

// SAFE: JSON.parse followed by schema validation (Joi)
app.post("/api/safe-config", (req, res) => {
  const rawBody = req.body.config;
  const parsed = JSON.parse(rawBody);
  const validated = schema.validate(parsed); // sanitizer
  if (validated.error) return res.status(400).json(validated.error);
  applyConfig(validated.value);
  res.json({ ok: true });
});

// SAFE: yaml.safeLoad
app.post("/api/safe-import", (req, res) => {
  const yamlData = req.body.data;
  const parsed = yaml.safeLoad(yamlData); // safe loader
  res.json(parsed);
});

function applyConfig(config: any) {}
const schema = { validate: (d: any) => ({ error: null, value: d }) };
