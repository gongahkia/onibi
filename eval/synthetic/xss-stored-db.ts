import express from "express";
import { Pool } from "pg";

const app = express();
app.use(express.json());
const db = new Pool();

app.post("/profile", async (req, res) => {
  const userId = req.params.id;
  const bio = req.body.bio as string;
  if (!bio) {
    res.status(400).send("missing bio");
    return;
  }
  await db.query("UPDATE users SET bio = $1 WHERE id = $2", [bio, userId]); // safe from SQLi
  res.send("updated");
});

app.get("/profile/:id", async (req, res) => {
  const result = await db.query("SELECT bio FROM users WHERE id = $1", [req.params.id]);
  if (result.rows.length === 0) {
    res.status(404).send("not found");
    return;
  }
  res.send(`<div class='bio'>${result.rows[0].bio}</div>`); // CWE-79: stored XSS, no HTML escaping
});
