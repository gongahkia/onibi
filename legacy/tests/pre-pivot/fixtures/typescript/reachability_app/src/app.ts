import express from "express";

const app = express();

function reachableHandler(req, res) {
  const name = req.body.name;
  return dangerousQuery(name);
}

function dangerousQuery(input) {
  return db.query("SELECT * FROM users WHERE name = '" + input + "'");
}

function deadEntry(req, res) {
  const orphan = req.body.name;
  return deadQuery(orphan);
}

function deadQuery(input) {
  return db.query("SELECT * FROM legacy WHERE name = '" + input + "'");
}

function neverCalled(input) {
  return db.query("SELECT * FROM ghosts WHERE name = '" + input + "'");
}

app.post("/users", reachableHandler);
