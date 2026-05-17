// NodeGoat-style regression: attacker-controlled redirect target from req.query.
// This locks coverage for the open-redirect miss documented in docs/examples/nodegoat.md.
export function vulnerableOpenRedirect(req, res) {
  const { url: target } = req.query;
  return res.redirect(target);
}

// Safe neighbor: fixed constant redirect target.
export function safeConstantRedirect(_req, res) {
  return res.redirect("/dashboard");
}
