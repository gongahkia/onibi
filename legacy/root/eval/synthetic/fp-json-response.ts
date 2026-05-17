import express from "express";

const app = express();

app.get("/search", (req, res) => {
  const search = req.query.search as string;
  if (!search) {
    res.status(400).send("missing search");
    return;
  }
  res.json({ query: search, results: [] }); // safe: res.json sets Content-Type: application/json
});
