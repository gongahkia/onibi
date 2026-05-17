import express from "express";
import fs from "fs";
import path from "path";

const app = express();
const uploadsDir = "/app/uploads";

app.get("/files/:filename", (req, res) => {
  const filename = req.params.filename;
  const filepath = path.join(uploadsDir, filename); // CWE-22: no containment check
  fs.readFile(filepath, (err, data) => {
    if (err) {
      res.status(404).send("not found");
      return;
    }
    res.send(data);
  });
});
