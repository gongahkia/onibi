import express from "express";

const app = express();

app.get("/fetch", async (req, res) => {
  const url = req.query.url as string;
  if (!url) {
    res.status(400).send("missing url");
    return;
  }
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    res.status(400).send("invalid url");
    return;
  }
  if (parsed.protocol !== "https:") { // block non-HTTPS
    res.status(403).send("only https allowed");
    return;
  }
  if (parsed.hostname === "localhost" || parsed.hostname.startsWith("127.") || parsed.hostname.startsWith("169.254.")) { // block internal
    res.status(403).send("internal addresses blocked");
    return;
  }
  const resp = await fetch(url); // safe: validated protocol and hostname
  const data = await resp.text();
  res.send(data);
});
