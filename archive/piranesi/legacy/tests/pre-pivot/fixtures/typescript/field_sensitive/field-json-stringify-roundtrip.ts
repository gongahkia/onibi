declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const json = JSON.stringify(req.body.profile);
  const parsed = JSON.parse(json);
  db.query(parsed.bio);
}
