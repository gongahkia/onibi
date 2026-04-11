declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const parsed = JSON.parse(req.body.data);
  db.query(parsed.sql);
}
