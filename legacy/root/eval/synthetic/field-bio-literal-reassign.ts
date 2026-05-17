import express from "express";

const app = express();

app.post("/field-bio", (req, res) => {
  let { bio } = req.body;
  bio = "hello";
  res.send(bio); // GT-478
});
