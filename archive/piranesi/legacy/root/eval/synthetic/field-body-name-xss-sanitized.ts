import express from "express";

const app = express();

app.post("/field-name", (req, res) => {
  const { name } = req.body;
  const safeName = escapeHtml(name);
  res.send(safeName); // GT-468
});
