import express from "express";

const app = express();

app.post("/field-rest", async (req, res) => {
  const { safe, ...rest } = req.body;
  await db.query(rest.id); // GT-479
  res.send("ok");
});
