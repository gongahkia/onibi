import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/orders/:id", async (req, res) => {
  const id = req.params.id;
  if (!id) {
    res.status(400).send("missing id");
    return;
  }
  const result = await client.query("SELECT * FROM orders WHERE id = " + id); // CWE-89: string concat in pg query
  res.json(result.rows);
});
