import express from "express";

const app = express();

app.post("/field-user-email", async (req, res) => {
  const email = req.body.user.email;
  await db.query(email); // GT-480
  res.send("ok");
});
