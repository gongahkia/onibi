import express from "express";
import { exec } from "child_process";

const app = express();
const ENABLE_LEGACY_EXEC = false; // permanently disabled feature flag

app.get("/run", (req, res) => {
  const cmd = req.query.cmd as string;
  if (!cmd) {
    res.status(400).send("missing cmd");
    return;
  }
  if (ENABLE_LEGACY_EXEC) { // dead code: always false
    exec(cmd, (err, stdout) => { // CWE-78 pattern but unreachable
      res.send(stdout);
    });
    return;
  }
  // safe path: allowlisted commands only
  const allowed = ["status", "version", "health"];
  if (allowed.includes(cmd)) {
    res.send(`result: ${cmd}`);
  } else {
    res.status(403).send("forbidden");
  }
});
