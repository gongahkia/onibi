import { db } from "./db";
import { escape } from "./sanitizers";

export function parameterizedQuery(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  // @piranesi-expect-clean: this parameterized query is safe
  return db.query("SELECT * FROM users WHERE id = $1", [userId]);
}

export function sanitizedInput(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  const safeUserId = escape(userId);
  // @piranesi-expect-clean: sanitized input does not reach a raw SQL sink
  return db.query("SELECT * FROM users WHERE id = '" + safeUserId + "'");
}

export function deadCode(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  if (false) {
    // @piranesi-expect-clean: this dead-code branch is unreachable
    return db.query("SELECT * FROM users WHERE id = '" + userId + "'");
  }
  return "ok";
}
