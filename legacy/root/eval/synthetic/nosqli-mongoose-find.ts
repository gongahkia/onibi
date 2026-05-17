export function handler(req, User) {
  const filter = req.body;
  return User.find(filter);
}
