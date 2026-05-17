import path from "node:path";
import { recall, remember } from "./selection";

const ROOT = "/srv/reports";

export function saveSelection(
  req: { params: { id: string }; body: { doc: string } },
) {
  const raw = req.body.doc;
  const stored = raw.trim();
  remember(req.params.id, stored);
}

export function openSelection(
  req: { params: { id: string } },
  res: { sendFile(value: string): string },
) {
  const chosen = recall(req.params.id);
  const candidate = path.join(ROOT, chosen);
  return res.sendFile(candidate); // SINK
}
