import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/users", async (req, res) => {
  const order = req.query.order as string;
  const allowed = ["created_at", "email", "name"];
  if (!order || !allowed.includes(order)) {
    res.status(400).send("invalid order");
    return;
  }
  const result = await client.query(`SELECT * FROM users ORDER BY ${order}`); // safe: strict allowlist bounds ORDER BY input
  res.json(result.rows);
});
