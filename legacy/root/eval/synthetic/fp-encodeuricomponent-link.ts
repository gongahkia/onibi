import express from "express";

const app = express();

app.get("/jump", (req, res) => {
  const next = req.query.next as string;
  if (!next) {
    res.status(400).send("missing next");
    return;
  }
  const encoded = encodeURIComponent(next);
  res.send(`<a href="/continue?next=${encoded}">Continue</a>`); // safe: encodeURIComponent keeps input inside query-parameter context
});
