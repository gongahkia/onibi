import express from "express";
import fs from "fs";
import path from "path";

const app = express();
const BASE_DIR = "/app/uploads";

app.get("/files/:filename", (req, res) => {
  const filename = req.params.filename;
  if (!filename) {
    res.status(400).send("missing filename");
    return;
  }
  const resolved = path.resolve(BASE_DIR, filename); // canonicalize
  if (!resolved.startsWith(BASE_DIR + "/")) { // containment check
    res.status(403).send("forbidden");
    return;
  }
  fs.readFile(resolved, (err, data) => { // safe: path validated
    if (err) {
      res.status(404).send("not found");
      return;
    }
    res.send(data);
  });
});
