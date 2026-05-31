#!/usr/bin/env node
import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, rename, rm, stat } from "node:fs/promises";
import { dirname, join } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const manifest = Object.freeze({
  "forensics-image-test": Object.freeze({
    name: "forensics-image-test",
    title: "CFReDS Forensics Image Test image",
    portalUrl: "https://cfreds.nist.gov/all/DFIR_AB/ForensicsImageTestimage",
    fileId: "1Fd1pX1r4waRkD6Z2O8J5cRZyeSNU5-SY",
    filename: "2020JimmyWilson.E01",
    sizeBytes: 309818835,
    sha256: "6c18f662744d55e2769d9510f6173f04dab668c42b67ef27b675d22e628b4ed5"
  })
});

const requestedName = process.argv[2] ?? "forensics-image-test";
const selected = manifest[requestedName];

if (!selected) {
  console.error(
    `Unknown CFReDS case '${requestedName}'. Known cases: ${Object.keys(manifest).join(", ")}`
  );
  process.exit(64);
}

const targetDir = join(".kelpclaw", "datasets", "cfreds", selected.name);
const targetPath = join(targetDir, selected.filename);
const temporaryPath = `${targetPath}.download`;

try {
  await mkdir(targetDir, { recursive: true });

  if (await exists(targetPath)) {
    await verifyExistingFile(targetPath, selected);
    console.log(`ok ${targetPath}`);
    process.exit(0);
  }

  await rm(temporaryPath, { force: true });
  const firstUrl = `https://drive.usercontent.google.com/uc?id=${encodeURIComponent(selected.fileId)}&export=download`;
  const response = await fetchDownloadResponse(firstUrl);

  await mkdir(dirname(temporaryPath), { recursive: true });
  await pipeline(Readable.fromWeb(response.body), createWriteStream(temporaryPath));

  await verifyExistingFile(temporaryPath, selected);
  await rename(temporaryPath, targetPath);
  console.log(`ok ${targetPath}`);
} catch (error) {
  await rm(temporaryPath, { force: true }).catch(() => undefined);
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}

async function fetchDownloadResponse(url) {
  const response = await fetch(url, { redirect: "follow" });
  if (!response.ok) {
    throw new Error(`Download request failed with HTTP ${response.status} ${response.statusText}`);
  }

  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("text/html")) {
    if (!response.body) {
      throw new Error("Download response did not include a body.");
    }
    return response;
  }

  const html = await response.text();
  const confirmationUrl = parseGoogleDriveConfirmationUrl(html);
  if (!confirmationUrl) {
    throw new Error("Google Drive returned HTML without a downloadable confirmation form.");
  }

  const confirmed = await fetch(confirmationUrl, { redirect: "follow" });
  if (!confirmed.ok) {
    throw new Error(
      `Confirmed download failed with HTTP ${confirmed.status} ${confirmed.statusText}`
    );
  }
  if (!confirmed.body) {
    throw new Error("Confirmed download response did not include a body.");
  }
  return confirmed;
}

function parseGoogleDriveConfirmationUrl(html) {
  const action = /<form[^>]+id=["']download-form["'][^>]+action=["']([^"']+)["']/iu.exec(html)?.[1];
  if (!action) {
    return undefined;
  }

  const params = new URLSearchParams();
  for (const match of html.matchAll(/<input[^>]+type=["']hidden["'][^>]*>/giu)) {
    const input = match[0];
    const name = /name=["']([^"']+)["']/iu.exec(input)?.[1];
    const value = /value=["']([^"']*)["']/iu.exec(input)?.[1] ?? "";
    if (name) {
      params.set(name, decodeHtml(value));
    }
  }

  if (!params.has("confirm")) {
    return undefined;
  }
  return `${decodeHtml(action)}?${params.toString()}`;
}

async function verifyExistingFile(path, entry) {
  const metadata = await stat(path);
  if (metadata.size !== entry.sizeBytes) {
    throw new Error(`Size mismatch for ${path}: expected ${entry.sizeBytes}, got ${metadata.size}`);
  }

  const actualSha256 = await sha256File(path);
  if (actualSha256 !== entry.sha256) {
    throw new Error(`SHA-256 mismatch for ${path}: expected ${entry.sha256}, got ${actualSha256}`);
  }
}

async function sha256File(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

async function exists(path) {
  try {
    await stat(path);
    return true;
  } catch (error) {
    if (error && typeof error === "object" && "code" in error && error.code === "ENOENT") {
      return false;
    }
    throw error;
  }
}

function decodeHtml(input) {
  return input
    .replaceAll("&amp;", "&")
    .replaceAll("&quot;", '"')
    .replaceAll("&#39;", "'")
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">");
}
