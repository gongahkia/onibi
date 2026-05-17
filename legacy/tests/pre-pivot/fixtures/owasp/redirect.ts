import express from "express";

const app = express();

// VULNERABLE: open redirect from query param
app.get("/login/callback", (req, res) => {
  const returnUrl = req.query.returnUrl; // tainted
  res.redirect(returnUrl as string); // CWE-601 sink
});

// VULNERABLE: open redirect from body
app.post("/auth/complete", (req, res) => {
  const next = req.body.next; // tainted
  res.redirect(next); // CWE-601 sink
});

// VULNERABLE: Location header from user input
app.get("/goto", (req, res) => {
  const url = req.query.url; // tainted
  res.setHeader("Location", url as string); // CWE-601 sink
  res.status(302).end();
});

// SAFE: redirect to relative path only (startsWith check)
app.get("/safe-redirect", (req, res) => {
  const returnUrl = req.query.returnUrl as string;
  if (!returnUrl.startsWith("/")) { // sanitizer
    return res.status(400).send("invalid redirect");
  }
  res.redirect(returnUrl);
});

// SAFE: redirect to hardcoded URL
app.get("/home", (req, res) => {
  res.redirect("/dashboard");
});
