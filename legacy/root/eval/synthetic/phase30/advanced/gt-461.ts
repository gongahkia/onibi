function applyUpdates(target: any, updates: Array<[string, unknown]>) {
  for (const [path, value] of updates) {
    const parts = path.split(".");
    let cursor = target;
    while (parts.length > 1) {
      const part = parts.shift() as string;
      if (!cursor[part]) {
        cursor[part] = {};
      }
      cursor = cursor[part];
    }
    cursor[parts[0]] = value; // sink
  }
}

export function patch(req: { body: { updates: Array<[string, unknown]> } }) {
  const state: any = {};
  applyUpdates(state, req.body.updates);
  return state;
}
