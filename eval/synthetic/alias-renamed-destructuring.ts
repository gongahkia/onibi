export function handler(req: any, db: any): void {
  const payload = req.body;
  const { name: username } = payload;
  db.query(username);
}
