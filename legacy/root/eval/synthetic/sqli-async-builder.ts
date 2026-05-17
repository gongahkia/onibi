import express from "express";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

async function lookupTable(segment: string): Promise<string> {
  return `${segment}_events`;
}

app.get("/events", async (req, res) => {
  const tenant = req.query.tenant as string;
  if (!tenant) {
    res.status(400).send("missing tenant");
    return;
  }
  const table = await lookupTable(tenant);
  const result = await client.query(`SELECT * FROM ${table}`); // CWE-89: async helper builds tainted table name
  res.json(result.rows);
});
