export function handler(req: any, db: any): void {
  const { email } = req.body;
  db.query(email);
}
