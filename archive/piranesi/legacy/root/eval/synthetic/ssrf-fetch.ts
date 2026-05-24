import express from "express";

const app = express();

app.get("/proxy", async (req, res) => {
  const url = req.query.url as string;
  if (!url) {
    res.status(400).send("missing url");
    return;
  }
  const resp = await fetch(url); // CWE-918: unvalidated URL to fetch
  const body = await resp.text();
  res.send(body);
});
