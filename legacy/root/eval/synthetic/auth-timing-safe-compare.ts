import crypto from "crypto";

function authenticate(user, providedPassword) {
  return crypto.timingSafeEqual(Buffer.from(user.password), Buffer.from(providedPassword));
}
