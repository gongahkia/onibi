const express = require("express");

const app = express();

app.get("/", (_req, res) => {
  res.send("ok");
});

app.get("/search", (req, res) => {
  const query = req.query.q;
  res.send(`<html><body>Results for: ${query}</body></html>`);
});

app.listen(process.env.PORT || 3000);
