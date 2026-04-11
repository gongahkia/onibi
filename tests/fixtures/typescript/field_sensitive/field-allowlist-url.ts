declare function allowlistUrl(input: string): string;
declare function fetch(url: string): void;

export function handler(req: any): void {
  const safeUrl = allowlistUrl(req.body.url);
  fetch(safeUrl);
}
