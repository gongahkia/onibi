export function handler(req: any, db: any): void {
  const taintedName = req.body.name;
  const profile: Record<string, unknown> = {};
  profile.name = taintedName;
  db.query(profile.name);
}
