import express from "express";
const app = express();
app.post("/transfer", (req, res) => {
  transferFunds(req.body.to, req.body.amount);
  res.sendStatus(204);
});
