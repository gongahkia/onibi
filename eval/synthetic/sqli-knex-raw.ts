import express from "express";
import Knex from "knex";

const app = express();
const knex = Knex({ client: "pg", connection: process.env.DATABASE_URL });

app.get("/items", async (req, res) => {
  const sort = req.query.sort as string;
  if (!sort) {
    res.status(400).send("missing sort");
    return;
  }
  const results = await knex.raw(`SELECT * FROM items ORDER BY ${sort}`); // CWE-89: template literal in knex.raw
  res.json(results.rows);
});
