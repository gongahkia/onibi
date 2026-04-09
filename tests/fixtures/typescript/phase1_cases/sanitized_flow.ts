import { db } from "./db";
import { escape } from "./sanitizers";

export function sanitizedFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  const escapedUserId = escape(userId);
  const sql = "SELECT * FROM users WHERE id = '" + escapedUserId + "'";
  // @piranesi-expect-clean: this escaped query is safe
  return db.query(sql);
}
