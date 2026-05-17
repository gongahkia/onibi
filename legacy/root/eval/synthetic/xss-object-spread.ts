import express from "express";

const app = express();

app.get("/banner", (req, res) => {
  const viewModel = { title: "Welcome", ...req.query };
  const message = viewModel.message as string;
  if (!message) {
    res.status(400).send("missing message");
    return;
  }
  res.send(`<main><h1>${viewModel.title}</h1><p>${message}</p></main>`); // CWE-79: object spread carries tainted field into view model
});
