export function handler(req, users) {
  const user = req.body.user;
  return users.findOne({ user });
}
