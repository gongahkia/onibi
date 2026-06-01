#!/usr/bin/env node
import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { mkdir, rename, rm, stat, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

const targetDir = join(".kelpclaw", "datasets", "cfreds", "hacking-case");

const files = [
  {
    filename: "4Dell Latitude CPi.E01",
    url: "https://cfreds-archive.nist.gov/images/4Dell%20Latitude%20CPi.E01",
    sizeBytes: 671094597
  },
  {
    filename: "4Dell Latitude CPi.E02",
    url: "https://cfreds-archive.nist.gov/images/4Dell%20Latitude%20CPi.E02",
    sizeBytes: 419384951
  },
  {
    filename: "TestAnswers.pdf",
    url: "https://cfreds-archive.nist.gov/images/TestAnswers.pdf",
    sizeBytes: 66916,
    sha256: "ee1795a7efe150ae3ecddaea2a1ee4ab2431a2c28ed3df009e759628bfda87f8"
  }
];

try {
  await mkdir(targetDir, { recursive: true });
  for (const file of files) {
    await fetchFile(file);
  }
  await writeFile(
    join(targetDir, "SOURCE.txt"),
    [
      "NIST CFReDS Hacking Case",
      "Archive page: https://cfreds-archive.nist.gov/Hacking_Case.html",
      "Expected acquisition image MD5 from official answers: AEE4FCD9301C03B3B054623CA261959A",
      "On SIFT, run: ewfverify '4Dell Latitude CPi.E01'",
      ""
    ].join("\n"),
    "utf8"
  );
  console.log(`ok ${targetDir}`);
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}

async function fetchFile(file) {
  const targetPath = join(targetDir, file.filename);
  const temporaryPath = `${targetPath}.download`;
  if (await exists(targetPath)) {
    await verifyFile(targetPath, file);
    console.log(`ok ${targetPath}`);
    return;
  }

  await rm(temporaryPath, { force: true });
  const response = await fetch(file.url, { redirect: "follow" });
  if (!response.ok) {
    throw new Error(`Download failed for ${file.filename}: HTTP ${response.status}`);
  }
  if (!response.body) {
    throw new Error(`Download response for ${file.filename} did not include a body.`);
  }
  await mkdir(dirname(temporaryPath), { recursive: true });
  await pipeline(Readable.fromWeb(response.body), createWriteStream(temporaryPath));
  await verifyFile(temporaryPath, file);
  await rename(temporaryPath, targetPath);
  console.log(`ok ${targetPath}`);
}

async function verifyFile(path, file) {
  const metadata = await stat(path);
  if (metadata.size !== file.sizeBytes) {
    throw new Error(`Size mismatch for ${path}: expected ${file.sizeBytes}, got ${metadata.size}`);
  }
  if (file.sha256) {
    const actualSha256 = await sha256File(path);
    if (actualSha256 !== file.sha256) {
      throw new Error(`SHA-256 mismatch for ${path}: expected ${file.sha256}, got ${actualSha256}`);
    }
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
