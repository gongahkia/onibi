export function handler(req: any, db: any): void {
  const merged = { ...req.body, safe: true };
  db.query(merged.sql);
}
