import jwt from "jsonwebtoken";

function issueToken(payload, secret) {
  return jwt.sign(payload, secret, { expiresIn: "1h", audience: "users" });
}
