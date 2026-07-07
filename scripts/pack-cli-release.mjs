#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cp, mkdir, mkdtemp, readFile, readdir, rename, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join, relative, resolve, sep } from "node:path";

const args = new Set(process.argv.slice(2));
const dryRun = args.has("--dry-run");
const smoke = args.has("--smoke") || !dryRun;
const root = resolve(new URL("..", import.meta.url).pathname);
const outRoot = resolve(root, ".kelpclaw", "release", "cli");
const deployDir = join(outRoot, "package");
const packDir = join(outRoot, "dist");
const internalDeps = [
  "@kelpclaw/adapters",
  "@kelpclaw/agent-hooks",
  "@kelpclaw/codegen",
  "@kelpclaw/evidence",
  "@kelpclaw/pi-cli",
  "@kelpclaw/policy",
  "@kelpclaw/web-intel",
  "@kelpclaw/workflow-spec"
];

await rm(outRoot, { recursive: true, force: true });
run("pnpm", ["--filter", "@kelpclaw/cli...", "build"]);
run("pnpm", ["--filter", "@kelpclaw/cli", "deploy", "--legacy", "--prod", deployDir]);
const packageDeps = await materializeBundledProductionPackages();
await rewritePackageJson(packageDeps);
run("npm", ["pack", "--dry-run"], { cwd: deployDir });

if (dryRun && !smoke) {
  console.log(JSON.stringify({ ok: true, dryRun: true, deployDir }));
  process.exit(0);
}

await mkdir(packDir, { recursive: true });
run("npm", ["pack", "--pack-destination", packDir], { cwd: deployDir });
const tarball = join(packDir, `kelpclaw-cli-0.1.0.tgz`);
if (!existsSync(tarball)) {
  throw new Error(`missing packed tarball: ${tarball}`);
}
const bytes = await readFile(tarball);
const sha256 = createHash("sha256").update(bytes).digest("hex");
await smokeInstall(tarball);
console.log(JSON.stringify({ ok: true, tarball, tarballName: basename(tarball), sha256 }));

async function materializeBundledProductionPackages() {
  const nodeModules = join(deployDir, "node_modules");
  const flatNodeModules = join(deployDir, "node_modules.__flat__");
  const dependencyVersions = new Map();
  const bundledDependencies = [];

  await rm(flatNodeModules, { recursive: true, force: true });
  await mkdir(flatNodeModules, { recursive: true });

  for (const [name, source] of await listProductionPackageSources(nodeModules)) {
    const target = packagePath(flatNodeModules, name);
    await mkdir(target, { recursive: true });
    if (internalDeps.includes(name)) {
      await cp(join(source, "dist"), join(target, "dist"), { recursive: true, dereference: true });
      const pkg = JSON.parse(await readFile(join(source, "package.json"), "utf8"));
      const rewritten = rewriteBundledWorkspacePackageJson(pkg, name);
      dependencyVersions.set(name, rewritten.version);
      await writeFile(
        join(target, "package.json"),
        `${JSON.stringify(rewritten, null, 2)}\n`,
        "utf8"
      );
    } else {
      await cp(source, target, {
        recursive: true,
        dereference: true,
        filter: (src) => !relative(source, src).split(sep).includes("node_modules")
      });
      const pkg = JSON.parse(await readFile(join(target, "package.json"), "utf8"));
      dependencyVersions.set(name, pkg.version);
    }
    bundledDependencies.push(name);
  }

  await rm(nodeModules, { recursive: true, force: true });
  await rename(flatNodeModules, nodeModules);
  bundledDependencies.sort((left, right) => left.localeCompare(right));
  return { bundledDependencies, dependencyVersions };
}

async function listProductionPackageSources(nodeModules) {
  const sources = new Map();
  for (const name of await listPackageNames(nodeModules)) {
    if (name === "@kelpclaw/cli") continue;
    sources.set(name, packagePath(nodeModules, name));
  }
  const pnpmNodeModules = join(nodeModules, ".pnpm", "node_modules");
  if (existsSync(pnpmNodeModules)) {
    for (const name of await listPackageNames(pnpmNodeModules)) {
      if (name === "@kelpclaw/cli") continue;
      if (!sources.has(name)) sources.set(name, packagePath(pnpmNodeModules, name));
    }
  }
  return [...sources.entries()].sort(([left], [right]) => left.localeCompare(right));
}

async function listPackageNames(root) {
  const names = [];
  for (const entry of await readdirSorted(root)) {
    if (entry.startsWith(".")) continue;
    if (entry.startsWith("@")) {
      for (const scopedEntry of await readdirSorted(join(root, entry))) {
        if (scopedEntry.startsWith(".")) continue;
        names.push(`${entry}/${scopedEntry}`);
      }
    } else {
      names.push(entry);
    }
  }
  return names.sort((left, right) => left.localeCompare(right));
}

async function readdirSorted(path) {
  return (await readdir(path)).sort((left, right) => left.localeCompare(right));
}

function packagePath(nodeModules, name) {
  const [scope, packageName] = name.startsWith("@") ? name.split("/") : [undefined, name];
  return scope === undefined
    ? join(nodeModules, packageName)
    : join(nodeModules, scope, packageName);
}

function rewriteBundledWorkspacePackageJson(pkg, expectedName) {
  if (pkg.name !== expectedName) throw new Error(`unexpected bundled package: ${pkg.name}`);
  const next = {
    name: pkg.name,
    version: pkg.version,
    private: false,
    type: pkg.type,
    main: "./dist/index.js",
    types: "./dist/index.d.ts",
    exports: {
      ".": {
        types: "./dist/index.d.ts",
        default: "./dist/index.js"
      }
    }
  };
  if (pkg.bin !== undefined) next.bin = pkg.bin;
  const deps = {};
  for (const [name, version] of Object.entries(pkg.dependencies ?? {})) {
    if (internalDeps.includes(name)) {
      deps[name] = pkg.version;
    } else {
      deps[name] = version;
    }
  }
  if (Object.keys(deps).length > 0) next.dependencies = deps;
  return next;
}

async function rewritePackageJson({ bundledDependencies, dependencyVersions }) {
  const path = join(deployDir, "package.json");
  const pkg = JSON.parse(await readFile(path, "utf8"));
  validateSourcePackage(pkg);
  pkg.private = false;
  pkg.types = "./dist/index.d.ts";
  pkg.files = ["dist", "README.md", "LICENSE"];
  pkg.bundledDependencies = bundledDependencies;
  pkg.dependencies = Object.fromEntries(
    bundledDependencies
      .map((name) => [name, dependencyVersions.get(name)])
      .sort(([left], [right]) => left.localeCompare(right))
  );
  delete pkg.devDependencies;
  await writeFile(path, `${JSON.stringify(pkg, null, 2)}\n`, "utf8");
}

function validateSourcePackage(pkg) {
  const required = ["description", "license", "repository", "homepage", "bugs", "bin", "files"];
  for (const field of required) {
    if (pkg[field] === undefined) throw new Error(`packages/cli/package.json missing ${field}`);
  }
  if (pkg.private !== false) throw new Error("CLI package must be public");
  if (pkg.bin["kelp-claw"] !== "./dist/index.js") throw new Error("missing kelp-claw bin");
  for (const file of ["dist", "README.md", "LICENSE"]) {
    if (!pkg.files.includes(file)) throw new Error(`CLI package files missing ${file}`);
  }
  for (const dep of internalDeps) {
    if (pkg.dependencies?.[dep] !== "workspace:*")
      throw new Error(`unexpected CLI dependency boundary: ${dep}`);
  }
}

async function smokeInstall(tarball) {
  const prefix = await mkdtemp(join(tmpdir(), "kelpclaw-cli-install-"));
  try {
    run("npm", ["install", "--global", "--prefix", prefix, tarball]);
    const bin = join(prefix, "bin", "kelp-claw");
    run(bin, [
      "doctor",
      "--root",
      join(prefix, "doctor-root"),
      "--codex-bin",
      join(prefix, "missing-codex")
    ]);
  } finally {
    await rm(prefix, { recursive: true, force: true });
  }
}

function run(command, commandArgs, options = {}) {
  const result = spawnSync(command, commandArgs, {
    cwd: options.cwd ?? root,
    env: process.env,
    encoding: "utf8",
    stdio: "pipe"
  });
  if (result.status !== 0) {
    process.stdout.write(result.stdout);
    process.stderr.write(result.stderr);
    throw new Error(`${command} ${commandArgs.join(" ")} failed with ${result.status}`);
  }
  if (result.stdout.trim().length > 0) process.stdout.write(result.stdout);
  if (result.stderr.trim().length > 0) process.stderr.write(result.stderr);
}
