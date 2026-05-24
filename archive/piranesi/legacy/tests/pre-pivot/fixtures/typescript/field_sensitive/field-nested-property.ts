declare const db: { query(sql: string): void };

export function handler(req: any): void {
  const email = req.body.user.email;
  db.query(email);
}
