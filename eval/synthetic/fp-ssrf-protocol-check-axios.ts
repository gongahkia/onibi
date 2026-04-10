import express from "express";
import axios from "axios";

const app = express();

app.get("/fetch-safe", async (req, res) => {
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
  if (parsed.protocol !== "https:") {
    res.status(403).send("only https allowed");
    return;
  }
  if (
    parsed.hostname === "localhost" ||
    parsed.hostname.startsWith("127.") ||
    parsed.hostname.startsWith("169.254.")
  ) {
    res.status(403).send("internal addresses blocked");
    return;
  }
  const response = await axios.get(parsed.toString());
  res.send(response.data);
});
