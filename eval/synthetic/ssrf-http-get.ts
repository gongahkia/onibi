import express from "express";
import http from "http";

const app = express();
app.use(express.json());

app.post("/fetch", (req, res) => {
  const target = req.body.target as string;
  if (!target) {
    res.status(400).send("missing target");
    return;
  }
  const parsed = new URL(target);
  http.get(parsed, (upstream) => { // CWE-918: user-controlled URL to http.get
    let data = "";
    upstream.on("data", (chunk) => (data += chunk));
    upstream.on("end", () => res.send(data));
  });
});
