declare const db: { query(sql: string): void };

function passThrough<T>(value: T): T {
  return value;
}

export function handler(req: any): void {
  db.query(passThrough(req.body.id));
}
