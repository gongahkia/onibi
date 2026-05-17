declare const db: { query(sql: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const { id, name } = req.body;
  const safeName = escapeHtml(name);
  const query = `SELECT * FROM users WHERE id = '${id}' AND name = '${safeName}'`;
  db.query(query);
}
