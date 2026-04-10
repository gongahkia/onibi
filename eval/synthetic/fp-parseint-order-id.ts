import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/orders/:id", async (req, res) => {
  const parsed = parseInt(req.params.id, 10);
  if (Number.isNaN(parsed)) {
    res.status(400).send("invalid id");
    return;
  }
  const result = await client.query(`SELECT * FROM orders WHERE id = ${parsed}`); // safe: parseInt narrows to numeric literal
  res.json(result.rows);
});
