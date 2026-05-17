function setPath(target: any, path: string, value: unknown) {
  const parts = path.split(".");
  let cursor = target;
  for (let index = 0; index < parts.length - 1; index += 1) {
    const part = parts[index];
    if (!cursor[part]) {
      cursor[part] = {};
    }
    cursor = cursor[part];
  }
  cursor[parts[parts.length - 1]] = value; // sink
}

export function update(req: { body: { path: string; value: unknown } }) {
  const target: any = {};
  setPath(target, req.body.path, req.body.value);
  return target;
}
