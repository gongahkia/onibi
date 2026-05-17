export function stashLookup(
  req: { body: { name: string } },
  res: { locals: Record<string, string> },
) {
  res.locals.lookup = req.body.name.trim();
}
