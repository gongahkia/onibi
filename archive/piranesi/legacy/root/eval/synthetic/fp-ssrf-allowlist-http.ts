import express from "express";
import http from "http";

const app = express();

const ALLOWED_TARGETS: Record<string, string> = {
  docs: "https://docs.example.com/index.json",
  metrics: "http://metrics.internal/health",
};

app.get("/proxy/allowed", (req, res) => {
  const service = req.query.service as string;
  const target = ALLOWED_TARGETS[service];
  if (!target) {
    res.status(403).send("unknown service");
    return;
  }
  http.get(target, (upstream) => {
    upstream.pipe(res);
  });
});
