import express from "express";
import fs from "fs";

const app = express();

app.get("/welcome", (req, res) => {
  const name = req.query.name as string;
  if (!name) {
    res.status(400).send("missing name");
    return;
  }
  fs.readFile("/app/templates/header.html", "utf8", (err, header) => {
    if (err) {
      res.status(500).send("template error");
      return;
    }
    res.send(`${header}<div>${name}</div>`); // CWE-79: tainted value reaches sink inside fs.readFile callback
  });
});
