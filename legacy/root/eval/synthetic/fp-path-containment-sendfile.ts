import express from "express";
import path from "path";

const app = express();
const PUBLIC_DIR = "/srv/public";

app.get("/assets/:file", (req, res) => {
  const file = req.params.file;
  if (!file) {
    res.status(400).send("missing file");
    return;
  }
  const resolved = path.resolve(PUBLIC_DIR, file);
  if (!resolved.startsWith(PUBLIC_DIR + "/")) {
    res.status(403).send("forbidden");
    return;
  }
  res.sendFile(resolved); // safe: resolve + startsWith enforces directory containment
});
