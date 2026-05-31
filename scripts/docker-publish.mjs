#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";

const image = "ghcr.io/gongahkia/kelp-claw";
const ghToken = process.env.GH_TOKEN;

if (!ghToken) {
  process.stderr.write(
    "GH_TOKEN is required to publish ghcr.io/gongahkia/kelp-claw. Export a GitHub token with package write access and retry.\n"
  );
  process.exit(1);
}

const packageJson = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
const version = packageJson.version;

if (typeof version !== "string" || version.length === 0) {
  process.stderr.write("package.json must contain a non-empty version string.\n");
  process.exit(1);
}

const sha = runCapture("git", ["rev-parse", "--short", "HEAD"]).trim();
const versionTag = `${version}-${sha}`;
const tags = [`${image}:${versionTag}`, `${image}:latest`];

run("docker", ["login", "ghcr.io", "-u", "gongahkia", "--password-stdin"], {
  input: `${ghToken}\n`
});
run("docker", ["build", "-f", "Dockerfile.kelp", "-t", tags[0], "-t", tags[1], "."]);

for (const tag of tags) {
  run("docker", ["push", tag]);
}

process.stdout.write(`Published ${tags.join(" and ")}\n`);

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    input: options.input,
    stdio: options.input ? ["pipe", "inherit", "inherit"] : "inherit",
    encoding: "utf8"
  });
  if (result.error) {
    process.stderr.write(`${command} failed: ${result.error.message}\n`);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

function runCapture(command, args) {
  const result = spawnSync(command, args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"]
  });
  if (result.error) {
    process.stderr.write(`${command} failed: ${result.error.message}\n`);
    process.exit(1);
  }
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
  return result.stdout;
}
