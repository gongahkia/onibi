const ALLOWED_KEYS = new Set(["email", "status"]);

function sanitizeFilter(rawFilter) {
  const output = {};
  for (const [key, value] of Object.entries(rawFilter || {})) {
    if (ALLOWED_KEYS.has(key)) {
      output[key] = value;
    }
  }
  return output;
}

function findUsers(app, req) {
  const query = sanitizeFilter(req.query.filter || {});
  return app.service("users").find({ query });
}

module.exports = { findUsers };
