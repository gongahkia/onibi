import express from "express";
import createDOMPurify from "dompurify";
import { JSDOM } from "jsdom";

const app = express();
app.use(express.json());
const window = new JSDOM("").window;
const DOMPurify = createDOMPurify(window as any);

app.post("/bios", (req, res) => {
  const bio = req.body.bio as string;
  if (!bio) {
    res.status(400).send("missing bio");
    return;
  }
  const clean = DOMPurify.sanitize(bio);
  res.send(`<article>${clean}</article>`); // safe: DOMPurify sanitizes rich-text input before rendering
});
