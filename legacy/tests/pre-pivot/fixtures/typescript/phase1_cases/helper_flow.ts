import { db } from "./db";

function buildLookupQuery(userId: string): string {
  return "SELECT * FROM users WHERE id = '" + userId + "'";
}

function executeLookup(userId: string) {
  return db.query(buildLookupQuery(userId));
}

export function helperFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  // @piranesi-expect: CWE-89, source=req.body.userId, sink=db_1.db.query
  return executeLookup(userId);
}
