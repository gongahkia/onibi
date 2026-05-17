declare const res: { send(body: string): void };
declare function escapeHtml(input: string): string;

export function handler(req: any): void {
  const safeHtml = escapeHtml(req.body.html);
  const body = JSON.stringify({ html: safeHtml });
  res.send(body);
}
