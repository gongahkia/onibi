declare const res: { send(body: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const { x } = req.body;
  const safeX = escapeHtml(x);
  res.send(safeX);
}
