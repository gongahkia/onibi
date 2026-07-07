import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = await mkdtemp(join(tmpdir(), "kelp-pi-bundle-equivalence-"));
const runId = "pi-bundle-equivalence-smoke";
const dataDir = join(root, "data");
const workspace = join(root, "workspace");
const bundleDir = join(root, "bundle");
const bin = join(repoRoot, "zig-out", "bin", "kelp-pi");

const verifierRequiredFiles = [
  "index.html",
  "result.json",
  "compatibility.json",
  "policy-decisions.json",
  "redaction-report.json",
  "manifest.json",
  "manifest.sig",
  "manifest.pub.json",
  "attestation.json",
  "attestation.sig"
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

async function fileExists(path) {
  try {
    await readFile(path);
    return true;
  } catch {
    return false;
  }
}

async function bundleContract() {
  const manifest = JSON.parse(await readFile(join(bundleDir, "manifest.json"), "utf8"));
  const attestation = JSON.parse(await readFile(join(bundleDir, "attestation.json"), "utf8"));
  const result = JSON.parse(await readFile(join(bundleDir, "result.json"), "utf8"));
  const compatibility = JSON.parse(await readFile(join(bundleDir, "compatibility.json"), "utf8"));
  const policy = JSON.parse(await readFile(join(bundleDir, "policy-decisions.json"), "utf8"));
  const manifestFiles = manifest.files.map((file) => file.path).sort();
  const attestationFiles = attestation.files.slice().sort();
  if (JSON.stringify(manifestFiles) !== JSON.stringify(attestationFiles)) {
    throw new Error("attestation files differ from manifest files");
  }
  return {
    manifestSchema: manifest.schemaVersion,
    runId: manifest.runId,
    algorithm: manifest.algorithm,
    attestationSchema: attestation.schemaVersion,
    attestationRunId: attestation.runId,
    manifestPath: attestation.manifest.path,
    signaturePath: attestation.manifest.signaturePath,
    publicKeyPath: attestation.manifest.publicKeyPath,
    requiredFiles: Object.fromEntries(
      await Promise.all(
        verifierRequiredFiles.map(async (file) => [
          file,
          manifestFiles.includes(file) || (await fileExists(join(bundleDir, file)))
        ])
      )
    ),
    resultStatus: result.status,
    resultOk: result.ok,
    compatibilityOk: compatibility.ok,
    policyPack: policy.policyPack
  };
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

  run(bin, ["keygen", "--data-dir", dataDir, "--label", "equivalence-smoke"]);
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
    runId
  ]);
  const verification = JSON.parse(run(bin, ["verify-bundle", bundleDir]));
  if (verification.ok !== true) {
    throw new Error(`bundle verification failed\n${JSON.stringify(verification, null, 2)}`);
  }

  const contract = await bundleContract();
  const missing = Object.entries(contract.requiredFiles)
    .filter(([, present]) => present !== true)
    .map(([file]) => file);
  if (missing.length > 0) {
    throw new Error(`bundle contract missing files: ${missing.join(", ")}`);
  }
  const expected = {
    manifestSchema: "1.0.0",
    runId,
    algorithm: "ed25519",
    attestationSchema: "1.0.0",
    attestationRunId: runId,
    manifestPath: "manifest.json",
    signaturePath: "manifest.sig",
    publicKeyPath: "manifest.pub.json",
    resultStatus: "succeeded",
    resultOk: true,
    compatibilityOk: true,
    policyPack: "appsec-agent-baseline"
  };
  for (const [key, value] of Object.entries(expected)) {
    if (contract[key] !== value) {
      throw new Error(`bundle contract ${key} mismatch: ${contract[key]} !== ${value}`);
    }
  }

  console.log("Zig Pi bundle equivalence smoke passed.");
} finally {
  if (process.env.KEEP_KELP_PI_EQUIV_TMP !== "1") {
    await rm(root, { recursive: true, force: true });
  }
}
