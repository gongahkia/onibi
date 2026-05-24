import express from "express";
import { execSync } from "child_process";

const app = express();
app.use(express.json());

app.post("/clone", (req, res) => {
  const repo = req.body.repo as string;
  if (!repo) {
    res.status(400).send("missing repo");
    return;
  }
  const output = execSync(`git clone ${repo}`); // CWE-78: template literal in execSync
  res.send(`cloned: ${output}`);
});
