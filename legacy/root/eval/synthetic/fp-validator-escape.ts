import express from "express";
import validator from "validator";

const app = express();

app.get("/comments", (req, res) => {
  const comment = req.query.comment as string;
  if (!comment) {
    res.status(400).send("missing comment");
    return;
  }
  const safe = validator.escape(comment);
  res.send(`<div class="comment">${safe}</div>`); // safe: validator.escape encodes HTML metacharacters
});
