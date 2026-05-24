import express from "express";
import http from "http";

const app = express();

app.get("/proxy/export", (req, res) => {
  const file = req.query.file as string;
  if (!file) {
    res.status(400).send("missing file");
    return;
  }
  const endpoint = `http://internal.service.local/export?file=${file}`;
  http.get(endpoint, (upstream) => {
    upstream.pipe(res);
  });
});
