app.post("/payment", requireAuth, async (req, res) => {
  await chargeCard(req.body.amount);
  res.sendStatus(204);
});
