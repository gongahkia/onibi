import express from "express";

const app = express();

app.post("/field-title", (req, res) => {
  res.render("page", { title: req.body.title }); // GT-476
});
