const express = require("express");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const app = express();
const docsDir = path.join(__dirname, "files");

app.use(express.json());

app.get("/search", (req, res) => {
  res.send(`<html><body>Search results for ${req.query.q}</body></html>`);
});

app.get("/files", (req, res) => {
  const fullPath = path.join(docsDir, req.query.file);
  res.json({ content: fs.readFileSync(fullPath, "utf8") });
});

app.get("/shell", (req, res) => {
  const output = execSync(`echo ${req.query.cmd}`, { encoding: "utf8" });
  res.json({ output });
});

app.get("/proxy", async (req, res) => {
  const response = await fetch(req.query.url, { signal: AbortSignal.timeout(2000) });
  res.json({ body: await response.text() });
});

app.listen(process.env.PORT || 3000);

