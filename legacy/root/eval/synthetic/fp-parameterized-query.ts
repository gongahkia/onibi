import express from "express";
import { Pool } from "pg";

const app = express();
const db = new Pool();

app.get("/users/:id", async (req, res) => {
  const userId = req.query.userId as string;
  if (!userId) {
    res.status(400).send("missing userId");
    return;
  }
  const result = await db.query( // safe: parameterized query with $1 placeholder
    "SELECT * FROM users WHERE id = $1",
    [userId]
  );
  res.json(result.rows);
});
