import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { claimSchema, type Claim, type EvidenceRef } from "../types/claim.js";
import { amcacheEntryToEvidenceRef, matchByPathOrHash, parseAmcacheOutput } from "./amcache.js";
import { matchByExecutable, parsePrefetchOutput, prefetchEntryToEvidenceRef } from "./prefetch.js";
import {
  matchByShimcachePath,
  parseShimcacheOutput,
  shimcacheEntryToEvidenceRef
} from "./shimcache.js";
import { matchBySrumApp, parseSrumOutput, srumEntryToEvidenceRef } from "./srum.js";
import { matchClaimToRows, parseTimelineCsv, timelineMatchToEvidenceRef } from "./timeline.js";

export { parseAmcacheOutput, matchByPathOrHash } from "./amcache.js";
export { hashEvidenceRow } from "./hashing.js";
export { parsePrefetchOutput, matchByExecutable } from "./prefetch.js";
export { parseShimcacheOutput, matchByShimcachePath } from "./shimcache.js";
export { parseSrumOutput, matchBySrumApp } from "./srum.js";
export { parseTimelineCsv, matchClaimToRows } from "./timeline.js";

const programExecutionProof = [
  "prefetch_entry",
  "amcache_execution_record",
  "shimcache_indicator",
  "srum_network_activity",
  "sysmon_process_create"
] as const;
const persistenceProof = ["registry-run-key", "scheduled-task", "service-create"] as const;
const networkProof = ["netflow-or-pcap"] as const;

export function linkEvidence(claim: Claim, caseDir: string): Claim {
  const files = listCaseFiles(caseDir);
  const additions: EvidenceRef[] = [];
  switch (claim.type) {
    case "program_execution":
      additions.push(...linkTimelineEvidence(claim, caseDir, files));
      additions.push(...linkPrefetchEvidence(claim, caseDir, files));
      additions.push(...linkAmcacheEvidence(claim, caseDir, files));
      additions.push(...linkShimcacheEvidence(claim, caseDir, files));
      additions.push(...linkSrumEvidence(claim, caseDir, files));
      break;
    case "file_presence":
    case "persistence":
    case "network_connection":
    case "timeline_ordering":
    case "user_activity":
      additions.push(...linkTimelineEvidence(claim, caseDir, files));
      break;
    default:
      additions.push(...linkTimelineEvidence(claim, caseDir, files));
      additions.push(...linkPrefetchEvidence(claim, caseDir, files));
      additions.push(...linkAmcacheEvidence(claim, caseDir, files));
      additions.push(...linkShimcacheEvidence(claim, caseDir, files));
      additions.push(...linkSrumEvidence(claim, caseDir, files));
      break;
  }

  const evidenceRefs = dedupeEvidenceRefs([...claim.evidenceRefs, ...additions]);
  return claimSchema.parse({
    ...claim,
    evidenceRefs,
    missingEvidence: missingEvidenceFor(claim.type, evidenceRefs, claim.missingEvidence)
  });
}

function linkTimelineEvidence(
  claim: Claim,
  caseDir: string,
  files: readonly string[]
): EvidenceRef[] {
  return files
    .filter((file) => isTimelineFile(file))
    .flatMap((file) => {
      const rows = parseTimelineCsv(readFileSync(file, "utf8"));
      return matchClaimToRows(claim.text, rows).map((match) =>
        timelineMatchToEvidenceRef(relativeArtifact(caseDir, file), match)
      );
    });
}

function linkPrefetchEvidence(
  claim: Claim,
  caseDir: string,
  files: readonly string[]
): EvidenceRef[] {
  return files
    .filter((file) => isPrefetchFile(file))
    .flatMap((file) => {
      const entries = parsePrefetchOutput(readFileSync(file, "utf8"));
      return matchByExecutable(claim.text, entries).map((entry) =>
        prefetchEntryToEvidenceRef(relativeArtifact(caseDir, file), entry)
      );
    });
}

function linkAmcacheEvidence(
  claim: Claim,
  caseDir: string,
  files: readonly string[]
): EvidenceRef[] {
  return files
    .filter((file) => isAmcacheFile(file))
    .flatMap((file) => {
      const entries = parseAmcacheOutput(readFileSync(file, "utf8"));
      return matchByPathOrHash(claim.text, entries).map((entry) =>
        amcacheEntryToEvidenceRef(relativeArtifact(caseDir, file), entry)
      );
    });
}

function linkShimcacheEvidence(
  claim: Claim,
  caseDir: string,
  files: readonly string[]
): EvidenceRef[] {
  return files
    .filter((file) => isShimcacheFile(file))
    .flatMap((file) => {
      const entries = parseShimcacheOutput(readFileSync(file, "utf8"));
      return matchByShimcachePath(claim.text, entries).map((entry) =>
        shimcacheEntryToEvidenceRef(relativeArtifact(caseDir, file), entry)
      );
    });
}

function linkSrumEvidence(claim: Claim, caseDir: string, files: readonly string[]): EvidenceRef[] {
  return files
    .filter((file) => isSrumFile(file))
    .flatMap((file) => {
      const entries = parseSrumOutput(readFileSync(file, "utf8"));
      return matchBySrumApp(claim.text, entries).map((entry) =>
        srumEntryToEvidenceRef(relativeArtifact(caseDir, file), entry)
      );
    });
}

function listCaseFiles(caseDir: string): string[] {
  if (!existsSync(caseDir)) {
    throw new Error(`case directory does not exist: ${caseDir}`);
  }
  const rootStat = statSync(caseDir);
  if (!rootStat.isDirectory()) {
    throw new Error(`case path is not a directory: ${caseDir}`);
  }
  const files: string[] = [];
  const visit = (directory: string): void => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) {
        visit(path);
      } else if (entry.isFile()) {
        files.push(path);
      }
    }
  };
  visit(caseDir);
  return files.sort((left, right) => left.localeCompare(right));
}

function isTimelineFile(path: string): boolean {
  const lower = path.toLowerCase();
  return lower.endsWith(".csv") && (lower.includes("timeline") || lower.includes("bodyfile"));
}

function isPrefetchFile(path: string): boolean {
  const lower = path.toLowerCase();
  return lower.includes("prefetch") && /\.(?:txt|csv|log|json)$/u.test(lower);
}

function isAmcacheFile(path: string): boolean {
  const lower = path.toLowerCase();
  return lower.includes("amcache") && /\.(?:txt|csv|log|json)$/u.test(lower);
}

function isShimcacheFile(path: string): boolean {
  const lower = path.toLowerCase();
  return (
    (lower.includes("shimcache") || lower.includes("appcompatcache")) &&
    /\.(?:txt|csv|log|json)$/u.test(lower)
  );
}

function isSrumFile(path: string): boolean {
  const lower = path.toLowerCase();
  return lower.includes("srum") && /\.(?:txt|csv|log|json)$/u.test(lower);
}

function relativeArtifact(caseDir: string, path: string): string {
  return relative(caseDir, path).split(sep).join("/");
}

function dedupeEvidenceRefs(refs: readonly EvidenceRef[]): EvidenceRef[] {
  return [
    ...new Map(
      refs.map((ref) => [`${ref.artifact}\0${ref.locator}\0${ref.supports}\0${ref.hash}`, ref])
    ).values()
  ];
}

function missingEvidenceFor(
  type: Claim["type"],
  refs: readonly EvidenceRef[],
  existing: readonly string[]
): string[] {
  const supports = new Set(refs.map((ref) => ref.supports));
  const required =
    type === "program_execution"
      ? programExecutionProof
      : type === "persistence"
        ? persistenceProof
        : type === "network_connection"
          ? networkProof
          : [];
  if (required.length === 0) {
    return [...new Set(existing)].sort();
  }
  if (required.some((item) => supports.has(item))) {
    const requiredSet = new Set<string>(required);
    return [...new Set(existing.filter((item) => !requiredSet.has(item)))].sort();
  }
  return [...new Set([...existing, ...required])].sort();
}
