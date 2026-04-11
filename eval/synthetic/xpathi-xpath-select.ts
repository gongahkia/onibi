import xpath from "xpath";

export function handler(req, doc) {
  return xpath.select(`//user[name='${req.body.username}']`, doc);
}
