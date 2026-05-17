export function stashLookup(
  req: { body: { name: string } },
  res: { locals: Record<string, string> },
) {
  const incoming = req.body.name;
  const trimmed = incoming.trim();
  res.locals.lookup = `%${trimmed}%`;
}
