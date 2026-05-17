import express from "express";
import Joi from "joi";
import { Client } from "pg";

const app = express();
const client = new Client();
client.connect();
const schema = Joi.object({ id: Joi.number().integer().required() });

app.get("/ledger", async (req, res) => {
  const { value, error } = schema.validate({ id: req.query.id });
  if (error) {
    res.status(400).send("invalid id");
    return;
  }
  const result = await client.query(`SELECT * FROM ledger WHERE id = ${value.id}`); // safe: Joi validates and coerces to integer
  res.json(result.rows);
});
