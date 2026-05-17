declare const res: { send(body: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const { name } = req.body;
  const safeName = escapeHtml(name);
  res.send(safeName);
}
