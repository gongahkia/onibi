import express from "express";
import { z } from "zod";

const app = express();

const schema = z.object({
  url: z.enum([
    "https://status.example.com/health",
    "https://status.example.com/metrics",
  ]),
});

app.get("/status-check", async (req, res) => {
  const parsed = schema.safeParse({ url: req.query.url });
  if (!parsed.success) {
    res.status(400).send("invalid url");
    return;
  }
  const response = await fetch(parsed.data.url); // safe: Zod enum constrains requests to allowlisted URLs
  res.send(await response.text());
});
