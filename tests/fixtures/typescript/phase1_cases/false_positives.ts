import { db } from "./db";
import { escape } from "./sanitizers";

// @piranesi-expect: CWE-89, source=req.body.userId, sink=db_1.db.query
export function parameterizedQuery(req: { body: { userId: string } }) {
  const userId = req.body.userId;

  return db.query("SELECT * FROM users WHERE id = $1", [userId]);
}

export function sanitizedInput(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  const safeUserId = escape(userId);

  return db.query("SELECT * FROM users WHERE id = '" + safeUserId + "'");
}

export function deadCode(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  if (false) {

    return db.query("SELECT * FROM users WHERE id = '" + userId + "'");
  }
  return "ok";
}
