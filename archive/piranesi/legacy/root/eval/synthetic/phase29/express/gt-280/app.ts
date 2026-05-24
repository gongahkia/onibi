import path from "node:path";

const ROOT = "/srv/docs";

function collapseNulls(name: string) {
  return name.replace(/\0/g, "");
}

function buildDocumentPath(name: string) {
  return path.join(ROOT, name);
}

export function download(
  req: { body: { file: string } },
  res: { sendFile(value: string): string },
) {
  const requested = req.body.file;
  const trimmed = requested.trim();
  const safeish = collapseNulls(trimmed);
  const fullPath = buildDocumentPath(safeish);
  return res.sendFile(fullPath); // SINK
}
