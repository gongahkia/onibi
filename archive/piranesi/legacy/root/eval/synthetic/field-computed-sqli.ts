import express from "express";

const app = express();

app.post("/field-computed", async (req, res) => {
  const key = req.body.key;
  await db.query(req.body[key]); // GT-477
  res.send("ok");
});
