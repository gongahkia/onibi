import express from "express";
import Knex from "knex";

const app = express();
const knex = Knex({ client: "pg", connection: process.env.DATABASE_URL });

app.get("/orders", async (req, res) => {
  const status = req.query.status as string;
  if (!status) {
    res.status(400).send("missing status");
    return;
  }
  const orders = await knex("orders").where({ status }).select("*"); // safe: knex builder parameterizes
  res.json(orders);
});
