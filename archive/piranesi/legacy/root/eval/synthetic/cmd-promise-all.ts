import express from "express";
import { exec } from "child_process";

const app = express();

async function defaultTool(): Promise<string> {
  return "nslookup";
}

app.get("/dns", async (req, res) => {
  const [tool, host] = await Promise.all([
    defaultTool(),
    Promise.resolve(req.query.host as string),
  ]);
  if (!host) {
    res.status(400).send("missing host");
    return;
  }
  const command = `${tool} ${host}`;
  exec(command, (err, stdout) => { // CWE-78: Promise.all path still reaches shell sink with tainted host
    if (err) {
      res.status(500).send("lookup failed");
      return;
    }
    res.send(stdout);
  });
});
