import express from "express";
import fs from "fs";
import path from "path";

const app = express();
const UPLOADS_DIR = "/app/uploads";

app.get("/preview", (req, res) => {
  const params = { ...req.query };
  const file = params.file as string;
  if (!file) {
    res.status(400).send("missing file");
    return;
  }
  const fullPath = path.join(UPLOADS_DIR, file);
  fs.readFile(fullPath, "utf8", (err, body) => { // CWE-22: spread keeps tainted path component
    if (err) {
      res.status(404).send("not found");
      return;
    }
    res.send(body);
  });
});
