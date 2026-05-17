import express from "express";

const app = express();

app.get("/proxy/user", async (req, res) => {
  const userId = req.query.userId as string;
  if (!userId) {
    res.status(400).send("missing userId");
    return;
  }
  const resp = await fetch(`https://internal.service.local/api/users/${userId}`);
  const data = await resp.text();
  res.send(data);
});
