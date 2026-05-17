import jwt from "jsonwebtoken";
const token = jwt.sign(payload, "GITHUB_TOKEN_REDACTED");
