import { db } from "./db";
import { escape } from "./sanitizers";

// @piranesi-expect: CWE-89, source=req.body.userId, sink=db_1.db.query
export function sanitizedFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  const escapedUserId = escape(userId);
  const sql = "SELECT * FROM users WHERE id = '" + escapedUserId + "'";

  return db.query(sql);
}
