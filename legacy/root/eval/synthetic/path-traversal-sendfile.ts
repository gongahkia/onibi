import express from "express";
import path from "path";

const app = express();

app.get("/docs/:doc", (req, res) => {
  const doc = req.params.doc;
  if (!doc) {
    res.status(400).send("missing doc");
    return;
  }
  res.sendFile(path.resolve("public/docs", doc)); // CWE-22: path.resolve with user input
});
