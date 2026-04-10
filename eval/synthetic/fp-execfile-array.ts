import express from "express";
import { execFile } from "child_process";

const app = express();

app.get("/convert", (req, res) => {
  const filename = req.query.filename as string;
  if (!filename) {
    res.status(400).send("missing filename");
    return;
  }
  execFile("convert", [filename, "output.pdf"], (err, stdout) => { // safe: execFile with array args, no shell
    if (err) {
      res.status(500).send("conversion failed");
      return;
    }
    res.send("converted");
  });
});
