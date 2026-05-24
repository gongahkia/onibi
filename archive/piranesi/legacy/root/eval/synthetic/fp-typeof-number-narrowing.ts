import express from "express";
import { Client } from "pg";

const app = express();
app.use(express.json());
const client = new Client();
client.connect();

app.post("/audit", async (req, res) => {
  const candidate = req.body.id as unknown;
  if (typeof candidate !== "number") {
    res.status(400).send("invalid id");
    return;
  }
  const result = await client.query(`SELECT * FROM audit_log WHERE id = ${candidate}`); // safe: runtime type narrowing excludes string injection
  res.json(result.rows);
});
