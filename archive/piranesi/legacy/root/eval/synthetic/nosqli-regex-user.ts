export function handler(req, users) {
  return users.find({ name: { $regex: req.query.q } });
}
