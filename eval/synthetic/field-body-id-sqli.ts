import express from "express";

const app = express();

app.post("/field-id", async (req, res) => {
  const { id } = req.body;
  await db.query(`SELECT * FROM users WHERE id = ${id}`); // GT-467
  res.send("ok");
});
