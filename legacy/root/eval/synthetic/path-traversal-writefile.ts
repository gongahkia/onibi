import express from "express";
import fs from "fs";
import path from "path";

const app = express();
app.use(express.json());

app.post("/upload", (req, res) => {
  const filename = req.body.filename as string;
  const content = req.body.content as string;
  if (!filename || !content) {
    res.status(400).send("missing filename or content");
    return;
  }
  const dest = path.join("/app/data", filename); // CWE-22: no containment check
  fs.writeFileSync(dest, content);
  res.send("written");
});
