export function handler(req, User, sanitize) {
  const filter = sanitize(req.body);
  return User.find(filter);
}
