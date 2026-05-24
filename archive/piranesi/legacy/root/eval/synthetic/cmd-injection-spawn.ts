import express from "express";
import { spawn } from "child_process";

const app = express();

app.get("/dns", (req, res) => {
  const host = req.query.host as string;
  if (!host) {
    res.status(400).send("missing host");
    return;
  }
  const proc = spawn("nslookup " + host, { shell: true }); // CWE-78: shell:true with concatenation
  let output = "";
  proc.stdout.on("data", (d) => (output += d));
  proc.on("close", () => res.send(output));
});
