import express from "express";
const app = express();
app.post("/login", (req, res) => {
  const user = authenticate(req.body.username, req.body.password);
  if (user) {
    req.session.userId = user.id;
    res.redirect("/dashboard");
  }
});
