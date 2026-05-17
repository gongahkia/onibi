declare const db: { query(sql: string): void };
declare function parameterize(input: string): string;

export function handler(req: any): void {
  const { email, token } = req.body;
  const safeEmail = parameterize(email);
  db.query(safeEmail);
  db.query(token);
}
