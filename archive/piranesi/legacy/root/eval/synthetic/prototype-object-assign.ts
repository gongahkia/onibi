export function handler(req: any): void {
  const key = req.body.key;
  const payload: Record<string, unknown> = {};
  payload[key] = req.body.value;
  Object.assign({}, payload);
}
