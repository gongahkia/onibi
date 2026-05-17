import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/invoices", async (req, res) => {
  const minimum = Number(req.query.minimum);
  if (!Number.isFinite(minimum)) {
    res.status(400).send("invalid minimum");
    return;
  }
  const result = await client.query(`SELECT * FROM invoices WHERE total > ${minimum}`); // safe: Number() coercion plus finite check blocks string injection
  res.json(result.rows);
});
