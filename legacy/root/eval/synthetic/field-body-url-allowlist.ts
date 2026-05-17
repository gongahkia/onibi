import express from "express";

const app = express();

app.post("/field-url", async (req, res) => {
  const safeUrl = allowlistUrl(req.body.url);
  await fetch(safeUrl); // GT-473
  res.send("ok");
});
