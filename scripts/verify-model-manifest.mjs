#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const defaultManifestPath = "models/manifest.toml";
const sha256Pattern = /^[a-f0-9]{64}$/u;

export function parseModelManifest(text) {
  const models = [];
  let current = null;
  for (const rawLine of text.split(/\n/u)) {
    const line = rawLine.trim();
    if (line.length === 0 || line.startsWith("#")) continue;
    if (line === "[[model]]") {
      if (current) models.push(current);
      current = { id: "", sha256: "", primary: false, releaseRequired: false };
      continue;
    }
    if (!current) continue;
    if (line.startsWith("id =")) current.id = quotedValue(line) ?? "";
    if (line.startsWith("sha256 =")) current.sha256 = quotedValue(line) ?? "";
    if (line.startsWith("primary =")) current.primary = booleanValue(line) ?? false;
    if (line.startsWith("release_required ="))
      current.releaseRequired = booleanValue(line) ?? false;
  }
  if (current) models.push(current);
  return models;
}

export function verifyReleaseModelManifestText(text, manifestPath = defaultManifestPath) {
  const releaseModels = parseModelManifest(text).filter(
    (model) => model.primary || model.releaseRequired
  );
  for (const model of releaseModels) {
    const id = model.id || "<missing-id>";
    if (model.sha256.length === 0) {
      throw new Error(`${manifestPath}: release-required model ${id} has blank sha256`);
    }
    if (!sha256Pattern.test(model.sha256)) {
      throw new Error(`${manifestPath}: release-required model ${id} has invalid sha256`);
    }
  }
  return releaseModels;
}

export async function verifyReleaseModelManifest(manifestPath = defaultManifestPath) {
  const text = await readFile(manifestPath, "utf8");
  return verifyReleaseModelManifestText(text, manifestPath);
}

function quotedValue(line) {
  return line.match(/=\s*"([^"]*)"/u)?.[1];
}

function booleanValue(line) {
  const value = line.match(/=\s*(true|false)\b/u)?.[1];
  if (value === "true") return true;
  if (value === "false") return false;
  return undefined;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const manifestPath = process.argv[2] ?? defaultManifestPath;
  try {
    const models = await verifyReleaseModelManifest(manifestPath);
    console.log(
      JSON.stringify({
        ok: true,
        manifestPath,
        releaseRequiredModels: models.map((model) => model.id)
      })
    );
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
