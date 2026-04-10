import express from "express";

const app = express();

app.get("/cards", (req, res) => {
  const name = req.query.name as string;
  if (!name) {
    res.status(400).send("missing name");
    return;
  }
  const card = `<article>${`<h1>${name}</h1>`}</article>`;
  res.send(card); // CWE-79: nested template literal reflects tainted input into HTML
});
