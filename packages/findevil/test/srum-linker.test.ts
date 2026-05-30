import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { linkEvidence, parseSrumOutput } from "../src/linker/index.js";
import type { Claim } from "../src/types/claim.js";

describe("SRUM evidence linker", () => {
  it("parses SRUM network activity and links program execution by app id hour", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-srum-"));
    await writeFile(join(directory, "srum.csv"), inlineSrum(), "utf8");

    expect(parseSrumOutput(inlineSrum())).toMatchObject([
      {
        appId: "evil.exe",
        application: "C:\\Users\\Public\\Downloads\\evil.exe",
        hour: "2026-05-30T10:00:00.000Z",
        timestamp: "2026-05-30T10:42:17Z",
        bytesSent: 1536,
        bytesReceived: 4096,
        sourceLocator: "srum:appid=evil.exe:hour=2026-05-30T10:00:00.000Z"
      }
    ]);

    const linked = linkEvidence(
      baseClaim({
        text: "evil.exe executed and contacted the network"
      }),
      directory
    );

    expect(linked.evidenceRefs).toHaveLength(1);
    expect(linked.evidenceRefs[0]).toMatchObject({
      artifact: "srum.csv",
      locator: "srum:appid=evil.exe:hour=2026-05-30T10:00:00.000Z",
      supports: "srum_network_activity"
    });
    expect(linked.evidenceRefs[0]?.hash).toMatch(/^sha256:[a-f0-9]{64}$/u);
    expect(linked.missingEvidence).toEqual([]);
  });
});

function inlineSrum(): string {
  return [
    "AppId,Application,TimeStamp,BytesSent,BytesReceived",
    "notepad.exe,C:\\Windows\\System32\\notepad.exe,2026-05-30T10:12:00Z,0,0",
    "evil.exe,C:\\Users\\Public\\Downloads\\evil.exe,2026-05-30T10:42:17Z,1536,4096"
  ].join("\n");
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-srum",
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
