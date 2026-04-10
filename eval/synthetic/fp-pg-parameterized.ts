import express from "express";
import { Pool } from "pg";

const app = express();
const pool = new Pool();

app.get("/accounts/:id", async (req, res) => {
  const id = req.params.id;
  if (!id) {
    res.status(400).send("missing id");
    return;
  }
  const result = await pool.query("SELECT * FROM accounts WHERE id = $1", [id]); // safe: pg parameterized query
  res.json(result.rows);
});
