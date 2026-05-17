import express from "express";

const app = express();

app.post("/field-html", (req, res) => {
  res.send(req.body.html); // GT-472
});
