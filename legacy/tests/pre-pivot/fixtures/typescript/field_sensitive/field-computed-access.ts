declare const db: { query(sql: string): void };

export function handler(req: any, key: string): void {
  const value = req.body[key];
  db.query(value);
}
