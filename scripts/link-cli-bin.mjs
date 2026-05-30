#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { chmod, lstat, mkdir, symlink } from "node:fs/promises";
import { basename, dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

if (process.platform === "win32") {
  process.exit(0);
}

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const binPath = resolve(repoRoot, "node_modules", ".bin", "kelp-claw");
const shimPath = resolve(repoRoot, "scripts", "link-cli-bin.mjs");
const targetPath = resolve(repoRoot, "packages", "cli", "dist", "index.js");

if (basename(process.argv[1] ?? "") === "kelp-claw") {
  const result = spawnSync(process.execPath, [targetPath, ...process.argv.slice(2)], {
    stdio: "inherit"
  });
  if (result.error) {
    throw result.error;
  }
  if (result.signal) {
    process.kill(process.pid, result.signal);
  }
  process.exit(result.status ?? 1);
}

try {
  await lstat(binPath);
  process.exit(0);
} catch (error) {
  if (error?.code !== "ENOENT") {
    throw error;
  }
}

await mkdir(dirname(binPath), { recursive: true });

try {
  await chmod(shimPath, 0o755);
  await symlink(relative(dirname(binPath), shimPath), binPath);
} catch (error) {
  if (error?.code !== "EEXIST") {
    throw error;
  }
}
