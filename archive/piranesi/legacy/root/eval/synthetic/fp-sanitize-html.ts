import express from "express";
import sanitizeHtml from "sanitize-html";

const app = express();

app.get("/preview", (req, res) => {
  const snippet = req.query.snippet as string;
  if (!snippet) {
    res.status(400).send("missing snippet");
    return;
  }
  const clean = sanitizeHtml(snippet, { allowedTags: ["b", "i", "strong"] });
  res.send(`<section>${clean}</section>`); // safe: sanitize-html strips unsafe tags and attributes
});
