import express from "express";

const app = express();

app.get("/thumb", async (req, res) => {
  const asset = req.query.asset as string;
  if (!asset) {
    res.status(400).send("missing asset");
    return;
  }
  const encoded = encodeURIComponent(asset);
  const response = await fetch(`https://cdn.example.com/assets/${encoded}`); // safe: encoded path segment on fixed public CDN origin
  res.send(await response.text());
});
