import express from "express";
import escapeHtml from "escape-html";

const app = express();

app.get("/greet", (req, res) => {
  const name = req.query.name as string;
  if (!name) {
    res.status(400).send("missing name");
    return;
  }
  const safe = escapeHtml(name); // sanitizer encodes <, >, &, ', "
  res.send(`<p>Hello, ${safe}</p>`); // safe: escaped before rendering
});
