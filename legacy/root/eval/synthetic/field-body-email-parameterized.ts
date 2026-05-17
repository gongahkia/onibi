import express from "express";

const app = express();

app.post("/field-email", async (req, res) => {
  const { email } = req.body;
  await db.query(parameterize(email)); // GT-469
  res.send("ok");
});
