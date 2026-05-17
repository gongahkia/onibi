declare function fetch(url: string): void;

export function handler(req: any): void {
  let { url } = req.body;
  url = "https://safe.example";
  fetch(url);
}
