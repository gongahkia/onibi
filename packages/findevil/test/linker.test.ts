import { existsSync, readdirSync } from "node:fs";
import { mkdtemp, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { linkEvidence } from "../src/linker/index.js";
import { parseAmcacheOutput } from "../src/linker/amcache.js";
import { parsePrefetchOutput } from "../src/linker/prefetch.js";
import { parseTimelineCsv } from "../src/linker/timeline.js";
import type { Claim } from "../src/types/claim.js";

describe("evidence linker", () => {
  it("parses baseline fixtures when present and otherwise links inline artifacts", async () => {
    const caseDir = await caseDirectory();
    const claim = baseClaim({
      text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe"
    });

    const linked = linkEvidence(claim, caseDir);

    expect(linked.evidenceRefs.map((ref) => ref.supports).sort()).toEqual([
      "amcache_execution_record",
      "file_present",
      "prefetch_entry"
    ]);
    expect(linked.evidenceRefs.every((ref) => /^sha256:[a-f0-9]{64}$/u.test(ref.hash))).toBe(true);
    expect(linked.missingEvidence).toEqual([]);
  });

  it("parses timeline, Prefetch, and Amcache formats", () => {
    expect(parseTimelineCsv(inlineTimeline()).at(0)).toMatchObject({
      rowNumber: 2,
      fields: {
        description: "File created C:\\Users\\Public\\Downloads\\evil.exe"
      }
    });
    expect(parsePrefetchOutput(inlinePrefetch())).toMatchObject([
      {
        executable: "evil.exe",
        runCount: 2
      }
    ]);
    expect(parseAmcacheOutput(inlineAmcache())).toMatchObject([
      {
        path: "c:\\users\\public\\downloads\\evil.exe"
      }
    ]);
  });
});

async function caseDirectory(): Promise<string> {
  const baseline = "fixtures/protocol-sift-baseline";
  if (
    existsSync(baseline) &&
    readdirSync(baseline, { withFileTypes: true }).some(
      (entry) => entry.isFile() && entry.name !== ".gitkeep"
    )
  ) {
    return baseline;
  }
  const directory = await mkdtemp(join(tmpdir(), "findevil-linker-"));
  await writeFile(join(directory, "timeline.csv"), inlineTimeline(), "utf8");
  await writeFile(join(directory, "prefetch.txt"), inlinePrefetch(), "utf8");
  await writeFile(join(directory, "amcache.txt"), inlineAmcache(), "utf8");
  return directory;
}

function inlineTimeline(): string {
  return [
    "Date,Source,Description,Filename",
    "2026-05-30,filesystem,File created C:\\Users\\Public\\Downloads\\evil.exe,C:\\Users\\Public\\Downloads\\evil.exe"
  ].join("\n");
}

function inlinePrefetch(): string {
  return [
    "File Name: EVIL.EXE-1234ABCD.pf",
    "Executable Name: evil.exe",
    "Run Count: 2",
    "Last Run Time: 2026-05-30 10:00:00Z"
  ].join("\n");
}

function inlineAmcache(): string {
  return [
    "Path: C:\\Users\\Public\\Downloads\\evil.exe",
    "SHA1: 0123456789abcdef0123456789abcdef01234567"
  ].join("\n");
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-0001",
    text: "evil.exe executed",
    type: "program_execution",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [
      "prefetch_entry",
      "amcache_execution_record",
      "shimcache_indicator",
      "sysmon_process_create"
    ],
    ...overrides
  };
}
