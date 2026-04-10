import express from "express";
import { Pool } from "pg";

const app = express();
const db = new Pool();

app.get("/users/:id", async (req, res) => {
  const id = parseInt(req.params.id, 10); // type coercion to integer
  if (isNaN(id)) {
    res.status(400).send("invalid id");
    return;
  }
  const result = await db.query(`SELECT * FROM users WHERE id = ${id}`); // safe: id is always numeric
  res.json(result.rows);
});
