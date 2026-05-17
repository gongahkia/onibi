declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const merged = { ...req.body };
  db.query(merged.name);
}
