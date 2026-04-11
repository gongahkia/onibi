const bios = new Map<string, string>();

export function saveProfile(
  req: { params: { id: string }; body: { bio: string } },
  res: { send(value: string): string },
) {
  const userId = req.params.id;
  const draft = req.body.bio;
  const stored = draft.trim();
  bios.set(userId, stored);
  return res.send("saved");
}

export function showProfile(
  req: { params: { id: string } },
  res: { send(value: string): string },
) {
  const userId = req.params.id;
  const persisted = bios.get(userId) ?? "";
  const markup = `<article>${persisted}</article>`;
  return res.send(markup); // SINK
}
