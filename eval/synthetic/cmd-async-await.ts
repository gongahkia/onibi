import express from "express";
import { exec } from "child_process";

const app = express();

async function buildArchiveCommand(filename: string): Promise<string> {
  const resolved = await Promise.resolve(filename);
  return `zip /tmp/archive.zip ${resolved}`;
}

app.get("/zip", async (req, res) => {
  const filename = req.query.filename as string;
  if (!filename) {
    res.status(400).send("missing filename");
    return;
  }
  const command = await buildArchiveCommand(filename);
  exec(command, (err) => { // CWE-78: async helper returns shell command built from tainted input
    if (err) {
      res.status(500).send("zip failed");
      return;
    }
    res.send("ok");
  });
});
