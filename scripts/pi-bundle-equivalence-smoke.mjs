import { spawnSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = await mkdtemp(join(tmpdir(), "kelp-pi-bundle-equivalence-"));
const runId = "pi-bundle-equivalence-smoke";
const tsRunsRoot = join(root, "ts-runs");
const tsRunDir = join(tsRunsRoot, runId);
const tsBundleDir = join(root, "ts-bundle");
const piDataDir = join(root, "pi-data");
const piWorkspace = join(root, "pi-workspace");
const piBundleDir = join(root, "pi-bundle");
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

async function writeJson(path, value) {
  await writeFile(path, `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

async function createTsFixtureRun() {
  await mkdir(tsRunDir, { recursive: true });
  await writeJson(join(tsRunDir, "skill.json"), {
    schemaVersion: "1.0.0",
    name: "pi-bundle-equivalence",
    contentHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111"
  });
  await writeJson(join(tsRunDir, "workflow.json"), {
    schemaVersion: "1.0.0",
    runId,
    steps: [{ id: "normalize-nuclei", status: "succeeded" }]
  });
  await writeJson(join(tsRunDir, "bom.json"), {
    schemaVersion: "1.0.0",
    packages: []
  });
  await writeFile(
    join(tsRunDir, "audit.jsonl"),
    `${JSON.stringify({
      ts: "2026-06-19T00:00:00Z",
      event: "scan.complete",
      msg: "fixture scan complete"
    })}\n`,
    "utf8"
  );
  await writeJson(join(tsRunDir, "policy-decisions.json"), {
    schemaVersion: "kelpclaw.pi.policy-decisions.v1",
    policyPack: "appsec-agent-baseline",
    decisions: []
  });
  await writeJson(join(tsRunDir, "result.json"), {
    schemaVersion: "kelpclaw.pi.bundle-result.v1",
    runId,
    ok: true,
    status: "succeeded",
    policyPack: "appsec-agent-baseline",
    mode: "fixture"
  });
  await writeJson(join(tsRunDir, "compatibility.json"), {
    schemaVersion: "kelpclaw.pi.compatibility.v1",
    ok: true,
    target: "kelp-pi",
    checks: [{ id: "fixture", status: "pass", message: "fixture run" }]
  });
}

async function createPiFixtureBundle() {
  await Promise.all(
    requiredDataDirs.map((name) => mkdir(join(piDataDir, name), { recursive: true }))
  );
  await mkdir(join(piWorkspace, "raw"), { recursive: true });
  const pi = ["run", "--quiet", "--manifest-path", "packages/pi-agent/Cargo.toml", "--"];
  run("cargo", [...pi, "keygen", "--data-dir", piDataDir]);
  run("cargo", [
    ...pi,
    "policy-check",
    "--data-dir",
    piDataDir,
    "--gate",
    "scanner-invocation",
    "--command",
    "scan nuclei https://app.example.test",
    "--host",
    "https://app.example.test",
    "--allowed"
  ]);
  const rawNuclei = join(piWorkspace, "raw", "nuclei.jsonl");
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
    piDataDir,
    "--input",
    rawNuclei,
    "--workspace",
    piWorkspace,
    "--raw-path",
    "raw/nuclei.jsonl"
  ]);
  run("cargo", [
    ...pi,
    "bundle",
    "assemble",
    "--data-dir",
    piDataDir,
    "--workspace",
    piWorkspace,
    "--output",
    piBundleDir,
    "--run-id",
    runId
  ]);
}

async function bundleContract(bundleDir) {
  const manifest = JSON.parse(await readFile(join(bundleDir, "manifest.json"), "utf8"));
  const attestation = JSON.parse(await readFile(join(bundleDir, "attestation.json"), "utf8"));
  const result = JSON.parse(await readFile(join(bundleDir, "result.json"), "utf8"));
  const compatibility = JSON.parse(await readFile(join(bundleDir, "compatibility.json"), "utf8"));
  const policy = JSON.parse(await readFile(join(bundleDir, "policy-decisions.json"), "utf8"));
  const manifestFiles = manifest.files.map((file) => file.path).sort();
  const attestationFiles = attestation.files.slice().sort();
  if (JSON.stringify(manifestFiles) !== JSON.stringify(attestationFiles)) {
    throw new Error(`attestation files differ from manifest files in ${bundleDir}`);
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

async function fileExists(path) {
  try {
    await readFile(path);
    return true;
  } catch {
    return false;
  }
}

function assertEquivalent(left, right) {
  const normalizedLeft = JSON.stringify(left, null, 2);
  const normalizedRight = JSON.stringify(right, null, 2);
  if (normalizedLeft !== normalizedRight) {
    throw new Error(`bundle contracts differ\nTS:\n${normalizedLeft}\nRust:\n${normalizedRight}`);
  }
}

try {
  await createTsFixtureRun();
  run(process.execPath, [
    "packages/cli/dist/index.js",
    "export-audit-bundle",
    runId,
    "--runs-dir",
    tsRunsRoot,
    "--out",
    tsBundleDir
  ]);
  await createPiFixtureBundle();

  for (const bundleDir of [tsBundleDir, piBundleDir]) {
    const verification = JSON.parse(
      run(process.execPath, [
        "packages/cli/dist/index.js",
        "verify-audit-bundle",
        bundleDir,
        "--strict"
      ])
    );
    if (verification.ok !== true) {
      throw new Error(
        `bundle verification failed for ${bundleDir}\n${JSON.stringify(verification, null, 2)}`
      );
    }
  }

  assertEquivalent(await bundleContract(tsBundleDir), await bundleContract(piBundleDir));
  console.log("Pi bundle equivalence smoke passed.");
} finally {
  if (process.env.KEEP_KELP_PI_EQUIV_TMP !== "1") {
    await rm(root, { recursive: true, force: true });
  }
}
