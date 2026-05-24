declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const { id } = req.body;
  db.query(id);
}
