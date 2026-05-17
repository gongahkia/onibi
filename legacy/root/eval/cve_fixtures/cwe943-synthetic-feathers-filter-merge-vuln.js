function buildQuery(req) {
  const base = { deleted: false };
  const userFilter = req.query.filter || {};
  return { ...base, ...userFilter };
}

function findUsers(app, req) {
  const query = buildQuery(req);
  return app.service("users").find({ query });
}

module.exports = { findUsers };
