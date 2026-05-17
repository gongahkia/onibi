declare const db: { query(sql: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const safeForHtml = escapeHtml(req.body.id);
  db.query(safeForHtml);
}
