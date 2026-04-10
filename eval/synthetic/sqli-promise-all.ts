import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

async function getTenant(): Promise<string> {
  return "acme";
}

app.get("/reports", async (req, res) => {
  const [column, tenant] = await Promise.all([
    Promise.resolve(req.query.column as string),
    getTenant(),
  ]);
  if (!column) {
    res.status(400).send("missing column");
    return;
  }
  const sql = `SELECT ${column} FROM reports WHERE tenant = '${tenant}'`;
  const result = await client.query(sql); // CWE-89: Promise.all mixes tainted and trusted values
  res.json(result.rows);
});
