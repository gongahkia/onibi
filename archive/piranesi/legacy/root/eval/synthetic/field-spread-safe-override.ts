import express from "express";

const app = express();

app.post("/field-safe", (req, res) => {
  const merged = { ...req.body, safe: "literal" };
  res.send(merged.safe); // GT-475
});
