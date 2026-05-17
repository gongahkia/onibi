import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/invoices", async (req, res) => {
  const { id, sort = "created_at" } = req.query as { id?: string; sort?: string };
  if (!id) {
    res.status(400).send("missing id");
    return;
  }
  const sql = `SELECT * FROM invoices WHERE id = ${id} ORDER BY ${sort}`;
  const result = await client.query(sql); // CWE-89: destructured values remain tainted in raw SQL
  res.json(result.rows);
});
