import express from "express";
import { runQuery } from "@test/shared-lib";

const app = express();

app.post("/users", (req, res) => {
  runQuery(req.body.id);
  res.send("ok");
});
