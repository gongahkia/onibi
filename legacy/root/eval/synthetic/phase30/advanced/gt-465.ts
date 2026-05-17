export function authorize(req: { headers: Record<string, string | undefined> }) {
  const provided = req.headers["x-signature"];
  const expected = process.env.WEBHOOK_SIGNATURE;
  if (provided === expected) { // sink
    return { ok: true };
  }
  return { ok: false };
}
