declare const db: { query(sql: string): void };

export function handler(req: any): void {
  req.body.items.map((item: any) => db.query(item.sql));
}
