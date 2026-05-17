declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const { safe, ...rest } = req.body;
  db.query(rest.id);
}
