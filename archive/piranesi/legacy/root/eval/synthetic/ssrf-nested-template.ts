import express from "express";

const app = express();

app.get("/status", async (req, res) => {
  const service = req.query.service as string;
  if (!service) {
    res.status(400).send("missing service");
    return;
  }
  const target = `http://internal.service.local/${`${service}`}/status`;
  const response = await fetch(target); // CWE-918: nested template literal builds internal request target
  res.send(await response.text());
});
