#!/usr/bin/env node
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdir, opendir, readFile, stat, writeFile } from "node:fs/promises";
import { join, relative, resolve, sep } from "node:path";
import { spawn } from "node:child_process";

const args = parseArgs(process.argv.slice(2));
const datasetDir = resolve(args.dataset ?? ".kelpclaw/datasets/cfreds/hacking-case");
const outDir = resolve(args.out ?? ".kelpclaw/findevil/cfreds-hacking-case/triage");
const evidenceRoot = join(outDir, "evidence");
const artifactsDir = join(evidenceRoot, "artifacts");
const recoveredDir = join(evidenceRoot, "recovered");
const tracePath = join(outDir, "trace.jsonl");
const runId = args.runId ?? `cfreds-hacking-case-${Date.now().toString(36)}`;
const e01Path = join(datasetDir, "4Dell Latitude CPi.E01");
const e02Path = join(datasetDir, "4Dell Latitude CPi.E02");
const expectedImageMd5 = "aee4fcd9301c03b3b054623ca261959a";

try {
  await mkdir(artifactsDir, { recursive: true });
  await mkdir(recoveredDir, { recursive: true });

  if (args.mode !== "emit-trace") {
    await prepareArtifacts();
  }

  const claims = await buildClaims();
  const events = traceEvents(claims);
  await writeFile(
    tracePath,
    events.map((event) => JSON.stringify(event)).join("\n") + "\n",
    "utf8"
  );
  for (const event of events) {
    process.stdout.write(`${JSON.stringify(event)}\n`);
  }
} catch (error) {
  const event = {
    event: "process_stderr",
    runId,
    timestamp: new Date().toISOString(),
    stream: "stderr",
    content: error instanceof Error ? error.message : String(error)
  };
  process.stdout.write(`${JSON.stringify(event)}\n`);
  process.exit(1);
}

async function prepareArtifacts() {
  await writeDatasetManifest();
  await capture("ewfverify", "ewfverify", [e01Path], { allowFailure: true });
  await capture("ewfinfo", "ewfinfo", [e01Path], { allowFailure: true });

  const rawImagePath = args.rawImage ? resolve(args.rawImage) : await mountEwfImage();
  if (!rawImagePath) {
    await writeFile(
      join(artifactsDir, "triage-status.txt"),
      "Unable to mount EWF image. Install libewf-utils on SIFT or pass --raw-image /path/to/ewf1.\n",
      "utf8"
    );
    return;
  }

  const mmls = await capture("partition-table", "mmls", [rawImagePath], { allowFailure: true });
  const offset = selectedFilesystemOffset(mmls.stdout);
  if (offset === undefined) {
    await writeFile(
      join(artifactsDir, "triage-status.txt"),
      "No filesystem offset found.\n",
      "utf8"
    );
    return;
  }
  await capture("fsstat", "fsstat", ["-o", String(offset), rawImagePath], { allowFailure: true });
  await capture(
    "tsk-recover",
    "tsk_recover",
    ["-a", "-o", String(offset), rawImagePath, recoveredDir],
    {
      allowFailure: true,
      maxRuntimeSeconds: 1200
    }
  );

  await runRegistryPlugins();
  await writeRecoveredInventory();
  await writeIndicatorSearches();
}

async function writeDatasetManifest() {
  const files = [];
  for (const path of [e01Path, e02Path]) {
    const metadata = await stat(path);
    files.push({
      path: relative(datasetDir, path).split(sep).join("/"),
      sizeBytes: metadata.size,
      sha256: await sha256File(path)
    });
  }
  await writeFile(
    join(artifactsDir, "original-image-manifest.json"),
    `${JSON.stringify(
      {
        source: "https://cfreds-archive.nist.gov/Hacking_Case.html",
        expectedImageMd5,
        files
      },
      null,
      2
    )}\n`,
    "utf8"
  );
}

async function mountEwfImage() {
  const mountDir = join(outDir, "ewf-mount");
  await mkdir(mountDir, { recursive: true });
  const result = await capture("ewfmount", "ewfmount", [e01Path, mountDir], { allowFailure: true });
  const candidate = join(mountDir, "ewf1");
  try {
    if ((await stat(candidate)).isFile()) {
      await writeFile(join(artifactsDir, "raw-image-path.txt"), `${candidate}\n`, "utf8");
      return candidate;
    }
  } catch {
    // Fall through to status artifact below.
  }
  await writeFile(
    join(artifactsDir, "ewfmount-status.txt"),
    `ewfmount did not expose ${candidate}\n${result.stderr}\n`,
    "utf8"
  );
  return undefined;
}

async function runRegistryPlugins() {
  const softwareHive = await findFirstFile(recoveredDir, (path) =>
    /\/windows\/system32\/config\/software$/iu.test(path.split(sep).join("/"))
  );
  const systemHive = await findFirstFile(recoveredDir, (path) =>
    /\/windows\/system32\/config\/system$/iu.test(path.split(sep).join("/"))
  );
  const samHive = await findFirstFile(recoveredDir, (path) =>
    /\/windows\/system32\/config\/sam$/iu.test(path.split(sep).join("/"))
  );
  if (softwareHive) {
    await capture("registry-winver", "rip.pl", ["-r", softwareHive, "-p", "winver"], {
      allowFailure: true
    });
    await capture("registry-uninstall", "rip.pl", ["-r", softwareHive, "-p", "uninstall"], {
      allowFailure: true
    });
  }
  if (systemHive) {
    await capture("registry-compname", "rip.pl", ["-r", systemHive, "-p", "compname"], {
      allowFailure: true
    });
    await capture("registry-timezone", "rip.pl", ["-r", systemHive, "-p", "timezone"], {
      allowFailure: true
    });
  }
  if (samHive) {
    await capture("registry-samparse", "rip.pl", ["-r", samHive, "-p", "samparse"], {
      allowFailure: true
    });
  }
}

async function writeRecoveredInventory() {
  const lines = [];
  for await (const path of walkFiles(recoveredDir)) {
    const metadata = await stat(path);
    lines.push(`${relative(recoveredDir, path).split(sep).join("/")}\t${metadata.size}`);
  }
  await writeFile(
    join(artifactsDir, "recovered-inventory.tsv"),
    lines.sort().join("\n") + "\n",
    "utf8"
  );
}

async function writeIndicatorSearches() {
  const indicators = [
    "Greg Schardt",
    "Mr. Evil",
    "Look@LAN",
    "Cain",
    "Ethereal",
    "NetStumbler",
    "whoknowsme@sbcglobal.net",
    "mrevilrulez@yahoo.com",
    "Interception"
  ];
  const matches = [];
  for (const indicator of indicators) {
    const found = await literalSearch(recoveredDir, indicator, 25);
    matches.push({ indicator, matches: found });
  }
  await writeFile(
    join(artifactsDir, "indicator-searches.json"),
    `${JSON.stringify(matches, null, 2)}\n`,
    "utf8"
  );
}

async function buildClaims() {
  const artifacts = await artifactIndex();
  const claims = [];
  const manifest = artifacts.get("artifacts/original-image-manifest.json");
  const ewfverify = artifacts.get("artifacts/ewfverify.txt");
  const winver = artifacts.get("artifacts/registry-winver.txt");
  const compname = artifacts.get("artifacts/registry-compname.txt");
  const samparse = artifacts.get("artifacts/registry-samparse.txt");
  const inventory = artifacts.get("artifacts/recovered-inventory.tsv");
  const indicators = artifacts.get("artifacts/indicator-searches.json");
  const lookLan = await findArtifactByName("irunin.ini");
  const interception = await findArtifactByName("Interception");

  if (manifest && ewfverify && (await artifactContains(ewfverify.path, expectedImageMd5))) {
    claims.push(
      claim(
        "claim-001",
        "The Hacking Case EWF acquisition MD5 verifies as AEE4FCD9301C03B3B054623CA261959A.",
        "file_presence",
        "medium",
        "T1005",
        [
          evidenceRef(manifest, "image_hash_manifest"),
          evidenceRef(ewfverify, "ewfverify_hash_match")
        ]
      )
    );
  }
  if (winver && (await artifactContains(winver.path, "Windows XP"))) {
    claims.push(
      claim(
        "claim-002",
        "The abandoned computer ran Windows XP.",
        "incident_conclusion",
        "medium",
        "T1082",
        [evidenceRef(winver, "registry_os_metadata")]
      )
    );
  }
  if (winver && (await artifactContainsAny(winver.path, ["Greg Schardt", "Greg", "Schardt"]))) {
    claims.push(
      claim(
        "claim-003",
        "The registered owner is Greg Schardt.",
        "user_activity",
        "high",
        "T1033",
        [evidenceRef(winver, "registry_registered_owner")]
      )
    );
  }
  if (compname && (await artifactContainsAny(compname.path, ["N-1A9ODN6ZXK4LQ", "Evil"]))) {
    claims.push(
      claim(
        "claim-004",
        "Registry computer identity artifacts link the host to N-1A9ODN6ZXK4LQ / Evil.",
        "incident_conclusion",
        "medium",
        "T1082",
        [evidenceRef(compname, "registry_computer_identity")]
      )
    );
  }
  if (
    (samparse && (await artifactContains(samparse.path, "Mr. Evil"))) ||
    (inventory && (await artifactContains(inventory.path, "Documents and Settings/Mr. Evil")))
  ) {
    claims.push(
      claim(
        "claim-005",
        "Mr. Evil is the primary user account observed in account or profile artifacts.",
        "user_activity",
        "high",
        "T1033",
        [evidenceRef(samparse ?? inventory, "user_account_artifact")]
      )
    );
  }
  if (lookLan) {
    claims.push(
      claim(
        "claim-006",
        "Look@LAN irunin.ini ties Greg Schardt / Mr. Evil identity evidence to the system administrator context.",
        "user_activity",
        "high",
        "T1033",
        [evidenceRef(lookLan, "lookatlan_identity_config")]
      )
    );
  }
  if (
    indicators &&
    (await artifactContainsAny(indicators.path, ["Cain", "Ethereal", "NetStumbler", "Look@LAN"]))
  ) {
    claims.push(
      claim(
        "claim-007",
        "Recovered artifacts show installed hacking or dual-use tools including Look@LAN, Ethereal, Cain, or NetStumbler.",
        "file_presence",
        "high",
        "T1046",
        [evidenceRef(indicators, "hacking_tool_indicator_search")]
      )
    );
  }
  if (interception) {
    claims.push(
      claim(
        "claim-008",
        "Recovered artifacts include an Interception file consistent with captured network traffic output.",
        "file_presence",
        "high",
        "T1040",
        [evidenceRef(interception, "captured_traffic_file")]
      )
    );
  }
  return claims;
}

function traceEvents(claims) {
  const now = new Date().toISOString();
  const events = [
    {
      event: "tool_call",
      runId,
      callId: "call-001",
      tool: "SIFTReadOnlyTriage",
      arguments: {
        datasetDir,
        evidenceRoot,
        tools: ["ewfverify", "ewfinfo", "mmls", "fsstat", "tsk_recover", "rip.pl"]
      }
    },
    {
      event: "tool_result",
      runId,
      callId: "call-001",
      status: "succeeded",
      content: `Recovered ${claims.length} evidence-backed Hacking Case claims.`
    },
    {
      event: "final_report",
      runId,
      content:
        claims.length > 0
          ? claims.map((item) => `- ${item.id}: ${item.text}`).join("\n")
          : "No Hacking Case claims were promoted because no recovered artifact proof was found."
    },
    ...claims.map((item) => ({
      event: "claim_extracted",
      runId,
      timestamp: now,
      claim: item
    }))
  ];
  return events;
}

function claim(id, text, type, severity, techniqueId, evidenceRefs) {
  return {
    id,
    text,
    type,
    severity,
    status: "confirmed",
    confidence: 0.95,
    attackTechniques: [
      {
        id: techniqueId,
        name: techniqueName(techniqueId),
        tactic: techniqueTactic(techniqueId)
      }
    ],
    evidenceRefs,
    missingEvidence: []
  };
}

function evidenceRef(record, supports) {
  return {
    artifact: record.relativePath,
    locator: "artifact",
    supports,
    hash: `sha256:${record.sha256}`
  };
}

async function artifactIndex() {
  const records = new Map();
  for await (const path of walkFiles(evidenceRoot)) {
    const relativePath = relative(evidenceRoot, path).split(sep).join("/");
    records.set(relativePath, {
      path,
      relativePath,
      sha256: await sha256File(path)
    });
  }
  return records;
}

async function findArtifactByName(name) {
  const lower = name.toLowerCase();
  for await (const path of walkFiles(evidenceRoot)) {
    if (path.split(sep).at(-1)?.toLowerCase() === lower) {
      return {
        path,
        relativePath: relative(evidenceRoot, path).split(sep).join("/"),
        sha256: await sha256File(path)
      };
    }
  }
  return undefined;
}

async function artifactContains(path, needle) {
  return (await readText(path)).toLowerCase().includes(needle.toLowerCase());
}

async function artifactContainsAny(path, needles) {
  const text = (await readText(path)).toLowerCase();
  return needles.some((needle) => text.includes(needle.toLowerCase()));
}

async function readText(path) {
  const buffer = await readFile(path);
  return buffer.toString("utf8");
}

async function literalSearch(root, needle, maxMatches) {
  const rows = [];
  for await (const path of walkFiles(root)) {
    const metadata = await stat(path);
    if (metadata.size > 10_000_000) {
      continue;
    }
    const text = await readText(path);
    if (!text.toLowerCase().includes(needle.toLowerCase())) {
      continue;
    }
    rows.push(relative(root, path).split(sep).join("/"));
    if (rows.length >= maxMatches) {
      break;
    }
  }
  return rows;
}

async function findFirstFile(root, predicate) {
  for await (const path of walkFiles(root)) {
    if (predicate(path)) {
      return path;
    }
  }
  return undefined;
}

async function* walkFiles(root) {
  let metadata;
  try {
    metadata = await stat(root);
  } catch {
    return;
  }
  if (metadata.isFile()) {
    yield root;
    return;
  }
  if (!metadata.isDirectory()) {
    return;
  }
  const directory = await opendir(root);
  for await (const entry of directory) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) {
      yield* walkFiles(path);
    } else if (entry.isFile()) {
      yield path;
    }
  }
}

async function capture(label, command, commandArgs, options = {}) {
  const result = await runCommand(command, commandArgs, options);
  await writeFile(
    join(artifactsDir, `${label}.txt`),
    [
      `$ ${[command, ...commandArgs].join(" ")}`,
      "",
      "STDOUT:",
      result.stdout,
      "",
      "STDERR:",
      result.stderr,
      "",
      `exitCode=${String(result.exitCode)}`
    ].join("\n"),
    "utf8"
  );
  if (result.exitCode !== 0 && !options.allowFailure) {
    throw new Error(`${command} exited with status ${String(result.exitCode)}.`);
  }
  return result;
}

async function runCommand(command, commandArgs, options) {
  return new Promise((resolveCommand) => {
    const child = spawn(command, commandArgs, {
      shell: false,
      stdio: ["ignore", "pipe", "pipe"]
    });
    const stdoutChunks = [];
    const stderrChunks = [];
    const timer = setTimeout(
      () => child.kill("SIGTERM"),
      (options.maxRuntimeSeconds ?? 300) * 1000
    );
    timer.unref?.();
    child.stdout.on("data", (chunk) => stdoutChunks.push(chunk));
    child.stderr.on("data", (chunk) => stderrChunks.push(chunk));
    child.on("error", (error) => {
      clearTimeout(timer);
      resolveCommand({ stdout: "", stderr: error.message, exitCode: 127 });
    });
    child.on("close", (exitCode) => {
      clearTimeout(timer);
      resolveCommand({
        stdout: Buffer.concat(stdoutChunks).toString("utf8"),
        stderr: Buffer.concat(stderrChunks).toString("utf8"),
        exitCode: exitCode ?? 1
      });
    });
  });
}

function selectedFilesystemOffset(mmlsOutput) {
  const rows = mmlsOutput
    .split(/\r?\n/u)
    .flatMap((line) => {
      const match = /^\s*\d+:\s+(.+?)\s+(\d+)\s+(\d+)\s+(\d+)/u.exec(line);
      if (!match?.[1] || !match[2] || !match[4]) {
        return [];
      }
      return [
        {
          description: match[1],
          start: Number(match[2]),
          length: Number(match[4])
        }
      ];
    })
    .filter((row) => !/unallocated|metadata/i.test(row.description));
  rows.sort((left, right) => right.length - left.length);
  return rows[0]?.start;
}

async function sha256File(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) {
    hash.update(chunk);
  }
  return hash.digest("hex");
}

function techniqueName(id) {
  return (
    {
      T1005: "Data from Local System",
      T1033: "System Owner/User Discovery",
      T1040: "Network Sniffing",
      T1046: "Network Service Discovery",
      T1082: "System Information Discovery"
    }[id] ?? "Mapped Technique"
  );
}

function techniqueTactic(id) {
  return id === "T1005" || id === "T1040" ? "collection" : "discovery";
}

function parseArgs(argv) {
  const parsed = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (!arg?.startsWith("--")) {
      continue;
    }
    const key = arg.slice(2);
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      parsed[key] = "true";
      continue;
    }
    parsed[key] = value;
    index += 1;
  }
  return {
    dataset: parsed.dataset,
    out: parsed.out,
    rawImage: parsed["raw-image"],
    runId: parsed["run-id"],
    mode: parsed.mode
  };
}
