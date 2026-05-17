import express from "express";
import axios from "axios";

const app = express();
app.use(express.json());

app.post("/probe", async (req, res) => {
  const slot = req.query.slot as string;
  const targets = req.body.targets as Record<string, { url?: string }>;
  const endpoint = targets?.[slot]?.url;
  if (!endpoint) {
    res.status(400).send("missing url");
    return;
  }
  const response = await axios.get(endpoint); // CWE-918: dynamic property lookup selects attacker-supplied URL
  res.send(response.data);
});
