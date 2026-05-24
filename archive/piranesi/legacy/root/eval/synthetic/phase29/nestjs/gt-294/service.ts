import fs from "node:fs";
import path from "node:path";

const ROOT = "/srv/docs";
const selections = new Map<string, string>();

export class DocumentService {
  save(userId: string, doc: string) {
    selections.set(userId, doc.trim());
  }

  show(userId: string) {
    const stored = selections.get(userId) ?? "index.txt";
    const fullPath = path.join(ROOT, stored);
    return fs.readFileSync(fullPath, "utf8"); // SINK
  }
}
