import express from "express";

const app = express();
app.use(express.json());

app.post("/avatar", async (req, res) => {
  const endpoint = req.body?.target?.url as string | undefined;
  if (!endpoint) {
    res.status(400).send("missing url");
    return;
  }
  const response = await fetch(endpoint); // CWE-918: optional chaining still yields attacker-controlled URL
  res.send(await response.text());
});
