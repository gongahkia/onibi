export function handler(req, users) {
  return users.find({ $where: "this.name === '" + req.body.name + "'" });
}
