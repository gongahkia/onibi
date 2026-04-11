export function mergeDirect(req: { body: Record<string, unknown> }) {
  const config: any = {};
  Object.assign(config, req.body); // sink
  return config;
}
