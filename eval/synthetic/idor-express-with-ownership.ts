import express from "express";
const app = express();
app.get("/api/orders/:id", async (req, res) => {
  const order = await Order.findOne({ where: { id: req.params.id, userId: req.user.id } });
  res.json(order);
});
