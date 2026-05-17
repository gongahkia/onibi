import { db } from "./db";

export function runCrossModuleLookup(userId: string) {
  const sql = "SELECT * FROM users WHERE id = '" + userId + "'";
  return db.query(sql);
}
