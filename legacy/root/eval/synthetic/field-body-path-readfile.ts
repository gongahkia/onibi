import express from "express";
import fs from "fs";

const app = express();

app.post("/field-path", (req, res) => {
  fs.readFile(req.body.path, "utf8", () => res.send("ok")); // GT-474
});
