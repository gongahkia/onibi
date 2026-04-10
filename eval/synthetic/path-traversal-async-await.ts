import express from "express";
import fs from "fs/promises";
import path from "path";

const app = express();
const ROOT = "/srv/documents";

async function resolveCandidate(file: string): Promise<string> {
  return Promise.resolve(file);
}

app.get("/documents", async (req, res) => {
  const file = req.query.file as string;
  if (!file) {
    res.status(400).send("missing file");
    return;
  }
  const candidate = await resolveCandidate(file);
  const body = await fs.readFile(path.join(ROOT, candidate), "utf8"); // CWE-22: async helper forwards tainted filename
  res.send(body);
});
