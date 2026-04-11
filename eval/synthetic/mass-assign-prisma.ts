app.post("/users", async (req, res) => {
  const user = await prisma.user.create({
    data: req.body,
  });
  res.json(user);
});
