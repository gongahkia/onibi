function merge(target: Record<string, any>, source: Record<string, any>): Record<string, any> {
  for (const key in source) {
    if (source[key] && typeof source[key] === "object") {
      target[key] = target[key] || {};
      merge(target[key], source[key]);
    } else {
      target[key] = source[key];
    }
  }
  return target;
}

export function handler(req: any): void {
  const key = req.body.key;
  const payload: Record<string, unknown> = {};
  payload[key] = req.body.value;
  merge({}, payload);
}
