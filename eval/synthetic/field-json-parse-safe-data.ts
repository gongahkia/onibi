import express from "express";

const app = express();

app.post("/field-json-data", async (req, res) => {
  const parsed = JSON.parse(req.body.data);
  const safeId = parseInt(parsed.id, 10);
  await db.query(safeId); // GT-481
  res.send("ok");
});
