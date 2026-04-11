app.delete("/admin/users/:id", requireAdmin, async (req, res) => {
  await User.destroy({ where: { id: req.params.id } });
  res.sendStatus(204);
});
