declare const res: { send(body: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const { title: rawTitle } = req.body;
  const safeTitle = escapeHtml(rawTitle);
  res.send(safeTitle);
}
