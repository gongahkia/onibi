import { spawnSync } from "node:child_process";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = await mkdtemp(join(tmpdir(), "kelp-pi-bundle-replay-"));
const dataDir = join(root, "data");
const workspace = join(root, "workspace");
const bundleDir = join(root, "bundle");
const stagedBundleDir = join(dataDir, "bundles", "pi-bundle-replay-smoke");
const importedBundleDir = join(root, "imported");
const exportEnvelope = join(root, "bundle-export-envelope.json");
const bin = join(repoRoot, "zig-out", "bin", "kelp-pi");

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
  await mkdir(join(workspace, "normalized"), { recursive: true });
  await writeFile(
    join(workspace, "normalized", "findings.json"),
    `${JSON.stringify({
      findings: [
        {
          title: "Missing security header",
          severity: "medium",
          target: "https://app.example.test"
        }
      ]
    })}\n`,
    "utf8"
  );

  run(bin, ["keygen", "--data-dir", dataDir, "--label", "replay-smoke"]);
  run(bin, [
    "policy",
    "sync",
    "--data-dir",
    dataDir,
    "--policy-pack-id",
    "appsec-agent-baseline@smoke-1",
    "--trust-epoch",
    "1",
    "--policy-json",
    JSON.stringify({ mode: "enforce" })
  ]);
  run(bin, [
    "policy",
    "sync",
    "--data-dir",
    dataDir,
    "--policy-pack-id",
    "appsec-agent-baseline@smoke-2",
    "--trust-epoch",
    "2",
    "--policy-json",
    JSON.stringify({ mode: "dry-run" })
  ]);
  const policyPull = JSON.parse(run(bin, ["policy", "pull", "--data-dir", dataDir]));
  if (
    policyPull.trust_epoch !== 2 ||
    !policyPull.known_policy_packs?.includes("appsec-agent-baseline@smoke-2")
  ) {
    throw new Error(`policy pull missing current pack\n${JSON.stringify(policyPull, null, 2)}`);
  }

  run(bin, [
    "index",
    "ingest",
    "--data-dir",
    dataDir,
    "--input",
    join(workspace, "normalized", "findings.json"),
    "--path",
    "evidence/fixture-target/normalized-findings.json"
  ]);
  const answer = JSON.parse(run(bin, ["ask", "security", "--data-dir", dataDir, "--top-k", "1"]));
  if (!answer.answer?.text || answer.citations?.length !== 1) {
    throw new Error(`ask did not return cited answer\n${JSON.stringify(answer, null, 2)}`);
  }

  run(bin, [
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
  const verification = JSON.parse(run(bin, ["verify-bundle", bundleDir]));
  if (verification.ok !== true) {
    throw new Error(`bundle verification failed\n${JSON.stringify(verification, null, 2)}`);
  }

  await cp(bundleDir, stagedBundleDir, { recursive: true });
  const exportPayload = run(bin, [
    "bundle",
    "export",
    "--data-dir",
    dataDir,
    "--bundle-id",
    "pi-bundle-replay-smoke",
    "--run-id",
    "pi-bundle-replay-smoke"
  ]);
  await writeFile(exportEnvelope, `${exportPayload}\n`, "utf8");
  run(bin, ["bundle", "import", "--input", exportEnvelope, "--out", importedBundleDir]);
  const importedVerification = JSON.parse(run(bin, ["verify-bundle", importedBundleDir]));
  if (importedVerification.ok !== true) {
    throw new Error(
      `imported bundle verification failed\n${JSON.stringify(importedVerification, null, 2)}`
    );
  }
  await readFile(join(importedBundleDir, "audit-log.jsonl"), "utf8");

  console.log("Zig Pi bundle replay smoke passed.");
} finally {
  if (process.env.KEEP_KELP_PI_REPLAY_TMP !== "1") {
    await rm(root, { recursive: true, force: true });
  }
}
