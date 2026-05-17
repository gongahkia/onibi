const db = {
  async read(_sku: string) {
    return 1;
  },
  async write(_sku: string, _value: number) {
  },
};

export async function reserve(req: { body: { sku: string } }) {
  const sku = req.body.sku;
  const stock = await db.read(sku);
  if (stock > 0) {
    await db.write(sku, stock - 1); // sink
    return { ok: true };
  }
  return { ok: false };
}
