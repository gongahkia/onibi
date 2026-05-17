export function handler(req, User, schema) {
  const filter = schema.parse(req.body);
  return User.find(filter);
}
