import { db } from "./db";

export function directFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  const sql = "SELECT * FROM users WHERE id = '" + userId + "'";
  // @piranesi-expect: CWE-89, source=req.body.userId, sink=db.query
  return db.query(sql);
}
