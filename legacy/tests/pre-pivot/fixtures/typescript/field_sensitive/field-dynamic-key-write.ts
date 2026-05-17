declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const obj: Record<string, string> = {};
  obj[req.body.key] = req.body.value;
  db.query(obj.x);
}
