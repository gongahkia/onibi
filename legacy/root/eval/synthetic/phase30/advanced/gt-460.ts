export function importSettings(req: { body: { payload: string } }) {
  const parsed = JSON.parse(req.body.payload);
  const settings: any = {};
  for (const key in parsed) {
    settings[key] = (parsed as any)[key]; // sink
  }
  return settings;
}
