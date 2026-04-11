app.post("/payment", async (req, res) => {
  await chargeCard(req.body.amount);
  res.sendStatus(204);
});
