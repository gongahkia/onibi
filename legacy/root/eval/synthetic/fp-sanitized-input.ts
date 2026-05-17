import express from "express";
import createDOMPurify from "dompurify";
import { JSDOM } from "jsdom";

const app = express();
app.use(express.json());
const window = new JSDOM("").window;
const DOMPurify = createDOMPurify(window as any);

app.post("/comments", async (req, res) => {
  const raw = req.body.comment as string;
  if (!raw) {
    res.status(400).send("missing comment");
    return;
  }
  const clean = DOMPurify.sanitize(raw); // sanitizer breaks taint chain
  // assume stored to DB as `clean`
  res.send(`<div>${clean}</div>`); // safe: sanitized before rendering
});
