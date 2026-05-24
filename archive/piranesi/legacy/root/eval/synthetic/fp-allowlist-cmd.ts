import express from "express";
import { exec } from "child_process";

const app = express();

app.get("/run", (req, res) => {
  const action = req.query.action as string;
  if (!action) {
    res.status(400).send("missing action");
    return;
  }
  const allowed = ["status", "health", "version"];
  if (!allowed.includes(action)) { // strict allowlist
    res.status(403).send("forbidden");
    return;
  }
  exec(action, (err, stdout) => { // safe: only allowlisted values reach here
    if (err) {
      res.status(500).send("error");
      return;
    }
    res.send(stdout);
  });
});
