import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = await mkdtemp(join(tmpdir(), "kelp-pi-bundle-replay-"));
const dataDir = join(root, "pi-data");
const workspace = join(root, "workspace");
const bundleDir = join(root, "pi-bundle");
const laptopBundleDir = join(root, "laptop", "pi-bundle");
const requiredDataDirs = [
  "corpus",
  "evidence",
  "bundles",
  "index",
  "audit",
  "keys",
  "policy",
  "scope"
];

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"]
  });
  if (result.status !== 0) {
    throw new Error(
      [
        `${command} ${args.join(" ")} exited ${result.status}`,
        result.stdout.trim(),
        result.stderr.trim()
      ]
        .filter(Boolean)
        .join("\n")
    );
  }
  return result.stdout.trim();
}

try {
  await Promise.all(
    requiredDataDirs.map((name) => mkdir(join(dataDir, name), { recursive: true }))
  );
  await mkdir(join(workspace, "raw"), { recursive: true });

  const pi = ["run", "--quiet", "--manifest-path", "packages/pi-agent/Cargo.toml", "--"];
  run("cargo", [...pi, "keygen", "--data-dir", dataDir]);
  run("cargo", [
    ...pi,
    "policy-check",
    "--data-dir",
    dataDir,
    "--gate",
    "outbound-network-request",
    "--host",
    "example.test:443",
    "--disallowed"
  ]);

  const rawNuclei = join(workspace, "raw", "nuclei.jsonl");
  await writeFile(
    rawNuclei,
    `${JSON.stringify({
      "template-id": "http-missing-security-headers",
      "matched-at": "https://app.example.test",
      info: { name: "Missing security header", severity: "medium" }
    })}\n`,
    "utf8"
  );
  run("cargo", [
    ...pi,
    "normalize",
    "nuclei",
    "--data-dir",
    dataDir,
    "--input",
    rawNuclei,
    "--workspace",
    workspace,
    "--raw-path",
    "raw/nuclei.jsonl"
  ]);
  run("cargo", [
    ...pi,
    "bundle",
    "assemble",
    "--data-dir",
    dataDir,
    "--workspace",
    workspace,
    "--output",
    bundleDir,
    "--run-id",
    "pi-bundle-replay-smoke"
  ]);

  await mkdir(dirname(laptopBundleDir), { recursive: true });
  await cp(bundleDir, laptopBundleDir, { recursive: true });
  const verification = JSON.parse(
    run(process.execPath, [
      "packages/cli/dist/index.js",
      "verify-audit-bundle",
      laptopBundleDir,
      "--profile",
      "reviewer"
    ])
  );
  const failures = [
    verification.ok === true ? "" : "bundle verification failed",
    verification.signature?.valid === true ? "" : "manifest signature invalid",
    verification.attestation?.valid === true ? "" : "attestation invalid"
  ].filter(Boolean);
  if (failures.length > 0) {
    throw new Error(`${failures.join("; ")}\n${JSON.stringify(verification, null, 2)}`);
  }

  await readFile(join(laptopBundleDir, "audit-log.jsonl"), "utf8");
  console.log("Pi bundle replay smoke passed.");
} finally {
  if (process.env.KEEP_KELP_PI_REPLAY_TMP !== "1") {
    await rm(root, { recursive: true, force: true });
  }
}
