import express from "express";
import fs from "fs/promises";
import path from "path";

const app = express();
const EXPORT_DIR = "/srv/exports";

app.get("/exports", async (req, res) => {
  const { file, ...rest } = req.query as { file?: string; preview?: string };
  if (!file) {
    res.status(400).send("missing file");
    return;
  }
  const fullPath = path.resolve(EXPORT_DIR, file);
  const body = await fs.readFile(fullPath, "utf8"); // CWE-22: destructuring/rest does not sanitize path input
  if (rest.preview === "1") {
    res.send(body.slice(0, 40));
    return;
  }
  res.send(body);
});
