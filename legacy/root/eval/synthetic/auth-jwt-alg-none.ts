import jwt from "jsonwebtoken";

function verifyToken(token, secret) {
  return jwt.verify(token, secret, { algorithms: ["none"] });
}
