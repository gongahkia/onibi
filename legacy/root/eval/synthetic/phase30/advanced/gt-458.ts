function deepMerge(target: any, source: any) {
  for (const key of Object.keys(source)) {
    if (typeof source[key] === "object" && source[key] !== null) {
      if (!target[key]) {
        target[key] = {};
      }
      deepMerge(target[key], source[key]); // sink
    } else {
      target[key] = source[key];
    }
  }
}

export function update(req: { body: Record<string, unknown> }) {
  const settings: any = { theme: "light" };
  deepMerge(settings, req.body);
  return settings;
}
