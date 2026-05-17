import fs from "node:fs";
import path from "node:path";

const ROOT = "/srv/docs";

function scrub(value: string) {
  return value.replace(/\0/g, "");
}

function buildPath(value: string) {
  return path.join(ROOT, value);
}

export class FileService {
  loadDocument(file: string) {
    const cleaned = scrub(file.trim());
    const fullPath = buildPath(cleaned);
    return fs.readFileSync(fullPath, "utf8"); // SINK
  }
}
