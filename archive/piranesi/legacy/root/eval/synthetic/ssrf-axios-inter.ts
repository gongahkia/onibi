import express from "express";
import axios from "axios";

const app = express();

async function fetchData(url: string): Promise<string> {
  const resp = await axios.get(url); // CWE-918: SSRF via inter-procedural taint
  return resp.data;
}

app.get("/data", async (req, res) => {
  const endpoint = req.query.endpoint as string;
  if (!endpoint) {
    res.status(400).send("missing endpoint");
    return;
  }
  const result = await fetchData(endpoint); // taint crosses function boundary
  res.send(result);
});
