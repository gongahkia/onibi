const express = require("express");
const router = express.Router();

router.get("/allocations", (req, res) => {
  if (typeof req.query.threshold !== "string") {
    res.status(400).send("bad request");
    return;
  }

  const userInput = String(req.query.threshold);

  const query = {
    userInput,
  };

  const filter = { stocks: { $gt: Number(userInput) } };

  return res.json({ query, filter });
});
