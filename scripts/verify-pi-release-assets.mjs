#!/usr/bin/env node
import { createHash } from "node:crypto";
import { verifyReleaseModelManifest } from "./verify-model-manifest.mjs";

const repo = process.env.KELP_PI_RELEASE_REPO ?? process.env.GITHUB_REPOSITORY ?? "gongahkia/kelp";
const tag = process.env.KELP_PI_RELEASE_TAG ?? process.env.RELEASE_TAG ?? "latest";
const asset = process.env.KELP_PI_RELEASE_ASSET ?? "kelp-pi-aarch64";
const modelManifest = process.env.KELP_PI_MODEL_MANIFEST ?? "models/manifest.toml";
const token = process.env.GITHUB_TOKEN ?? process.env.GH_TOKEN;

await verifyReleaseModelManifest(modelManifest);

const assetUrl = releaseDownloadUrl(repo, tag, asset);
const checksumUrl = releaseDownloadUrl(repo, tag, `${asset}.sha256`);

await headOk(assetUrl);
await headOk(checksumUrl);

const assetBytes = Buffer.from(await (await fetchOk(assetUrl)).arrayBuffer());
const checksumText = await (await fetchOk(checksumUrl)).text();
const expectedSha256 = checksumText.trim().split(/\s+/u)[0];
if (!/^[a-f0-9]{64}$/u.test(expectedSha256)) {
  throw new Error(`${asset}.sha256 does not start with a SHA-256 hex digest`);
}

const actualSha256 = createHash("sha256").update(assetBytes).digest("hex");
if (actualSha256 !== expectedSha256) {
  throw new Error(`release asset hash mismatch: expected ${expectedSha256}, found ${actualSha256}`);
}

if (tag !== "latest" && token) {
  await verifyGithubAssetDigest(repo, tag, asset, actualSha256);
}

console.log(
  JSON.stringify({
    ok: true,
    repo,
    tag,
    asset,
    url: assetUrl,
    sha256: actualSha256,
    sizeBytes: assetBytes.byteLength
  })
);

function releaseDownloadUrl(ownerRepo, releaseTag, name) {
  if (releaseTag === "latest") {
    return `https://github.com/${ownerRepo}/releases/latest/download/${encodeURIComponent(name)}`;
  }
  return `https://github.com/${ownerRepo}/releases/download/${encodeURIComponent(releaseTag)}/${encodeURIComponent(name)}`;
}

async function headOk(url) {
  const response = await fetch(url, { method: "HEAD", redirect: "manual" });
  if (response.status < 200 || response.status >= 400) {
    throw new Error(`HEAD ${url} returned ${response.status}`);
  }
}

async function fetchOk(url) {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`GET ${url} returned ${response.status}`);
  }
  return response;
}

async function verifyGithubAssetDigest(ownerRepo, releaseTag, name, sha256) {
  const response = await fetch(
    `https://api.github.com/repos/${ownerRepo}/releases/tags/${releaseTag}`,
    {
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "X-GitHub-Api-Version": "2022-11-28"
      }
    }
  );
  if (!response.ok) {
    throw new Error(`GitHub release API returned ${response.status}`);
  }
  const release = await response.json();
  const match = release.assets?.find((candidate) => candidate.name === name);
  if (!match) {
    throw new Error(`GitHub release API did not list ${name}`);
  }
  if (typeof match.digest === "string" && match.digest !== `sha256:${sha256}`) {
    throw new Error(`GitHub release API digest mismatch: ${match.digest}`);
  }
}
