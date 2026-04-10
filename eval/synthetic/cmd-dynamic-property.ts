import express from "express";
import { exec } from "child_process";

const app = express();
app.use(express.json());

app.post("/archive", (req, res) => {
  const field = req.query.field as string;
  const args = req.body as Record<string, string>;
  if (!field || !args || !args[field]) {
    res.status(400).send("missing target");
    return;
  }
  const target = args[field];
  exec(`tar -czf backup.tgz ${target}`, (err) => { // CWE-78: dynamic property access preserves taint
    if (err) {
      res.status(500).send("archive failed");
      return;
    }
    res.send("ok");
  });
});
