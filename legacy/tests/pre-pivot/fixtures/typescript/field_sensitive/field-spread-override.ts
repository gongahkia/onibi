declare const res: { send(body: string): void };

export function handler(req: any): void {
  const merged = { ...req.body, safe: "ok" };
  res.send(merged.safe);
}
