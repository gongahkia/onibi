import express from "express";
import { exec } from "child_process";

const app = express();

app.get("/convert", (req, res) => {
  const filename = req.query.filename as string;
  if (!filename) {
    res.status(400).send("missing filename");
    return;
  }
  const cmd = `convert ${filename} output.pdf`; // CWE-78: unsanitized user input in shell command
  exec(cmd, (err, stdout, stderr) => {
    if (err) {
      res.status(500).send(stderr);
      return;
    }
    res.send("converted");
  });
});
