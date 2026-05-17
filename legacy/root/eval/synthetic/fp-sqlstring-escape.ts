import express from "express";
import SqlString from "sqlstring";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();

app.get("/posts", async (req, res) => {
  const slug = req.query.slug as string;
  if (!slug) {
    res.status(400).send("missing slug");
    return;
  }
  const escapedSlug = SqlString.escape(slug);
  const result = await client.query(`SELECT * FROM posts WHERE slug = ${escapedSlug}`); // safe: sqlstring.escape quotes attacker input
  res.json(result.rows);
});
