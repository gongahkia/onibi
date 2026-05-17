import express from "express";
import escapeHtml from "escape-html";

const app = express();

app.get("/redirect", (req, res) => {
  const next = req.query.next as string;
  if (!next) {
    res.status(400).send("missing next");
    return;
  }
  const safe = escapeHtml(next);
  res.send(`<a href="/continue?next=${safe}">continue</a>`); // safe: HTML-escaped before interpolation
});
