import crypto from "crypto";
export function signToken(token: string) {
  return crypto.createHash("sha1").update(token).digest("hex");
}
