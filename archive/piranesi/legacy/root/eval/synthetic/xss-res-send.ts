import express from "express";

const app = express();

app.get("/search", (req, res) => {
  const q = req.query.q as string;
  if (!q) {
    res.status(400).send("missing q");
    return;
  }
  res.send(`<h1>Results for: ${q}</h1>`); // CWE-79: reflected XSS via template literal
});
