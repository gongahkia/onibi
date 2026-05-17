declare const db: { query(sql: string | number): void };

export function handler(req: any): void {
  const parsed = JSON.parse(req.body.data);
  const safeId = parseInt(parsed.id, 10);
  db.query(safeId);
}
