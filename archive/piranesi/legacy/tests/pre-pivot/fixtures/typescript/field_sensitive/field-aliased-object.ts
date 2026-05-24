declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const payload = req.body;
  const { id } = payload;
  db.query(id);
}
