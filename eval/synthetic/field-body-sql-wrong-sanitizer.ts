import express from "express";

const app = express();

app.post("/field-sql", async (req, res) => {
  const safeForHtml = escapeHtml(req.body.sql);
  await db.query(safeForHtml); // GT-471
  res.send("ok");
});
