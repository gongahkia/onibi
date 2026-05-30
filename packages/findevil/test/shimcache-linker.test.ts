import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { linkEvidence, parseShimcacheOutput } from "../src/linker/index.js";
import type { Claim } from "../src/types/claim.js";

describe("ShimCache evidence linker", () => {
  it("parses ShimCache rows and links program execution by path", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-shimcache-"));
    await writeFile(join(directory, "shimcache.csv"), inlineShimcache(), "utf8");

    expect(parseShimcacheOutput(inlineShimcache())).toMatchObject([
      {
        path: "c:\\windows\\system32\\notepad.exe",
        sourceLocator: "shimcache:row=2"
      },
      {
        path: "c:\\users\\public\\downloads\\evil.exe",
        lastModified: "2026-05-30T09:55:00Z",
        sourceLocator: "shimcache:row=3"
      }
    ]);

    const linked = linkEvidence(
      baseClaim({
        text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe"
      }),
      directory
    );

    expect(linked.evidenceRefs).toHaveLength(1);
    expect(linked.evidenceRefs[0]).toMatchObject({
      artifact: "shimcache.csv",
      locator: "shimcache:row=3",
      supports: "shimcache_indicator"
    });
    expect(linked.evidenceRefs[0]?.hash).toMatch(/^sha256:[a-f0-9]{64}$/u);
    expect(linked.missingEvidence).toEqual([]);
  });
});

function inlineShimcache(): string {
  return [
    "Last Modified,Path,Source",
    "2026-05-30T09:00:00Z,C:\\Windows\\System32\\notepad.exe,SYSTEM",
    "2026-05-30T09:55:00Z,C:\\Users\\Public\\Downloads\\evil.exe,SYSTEM"
  ].join("\n");
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-shimcache",
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
      "srum_network_activity",
      "sysmon_process_create"
    ],
    ...overrides
  };
}
