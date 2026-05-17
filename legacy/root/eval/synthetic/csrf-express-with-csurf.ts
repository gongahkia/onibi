import express from "express";
import csurf from "csurf";

const app = express();
app.use(csurf());
app.post("/transfer", (req, res) => {
  res.sendStatus(204);
});
