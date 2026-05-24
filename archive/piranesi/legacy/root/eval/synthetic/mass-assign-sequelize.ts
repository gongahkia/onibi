app.post("/users", async (req, res) => {
  const user = await User.create(req.body);
  res.json(user);
});
