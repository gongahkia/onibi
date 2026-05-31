import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { linkEvidence } from "../src/linker/index.js";
import {
  matchRegistryRunKey,
  matchRegistryScheduledTask,
  matchRegistryService,
  matchRegistryShimCache,
  parseRegistryJson
} from "../src/linker/registry.js";
import type { Claim } from "../src/types/claim.js";

describe("registry linker", () => {
  it("parses registry rows and matches run keys, services, scheduled tasks, and ShimCache", async () => {
    const file = await writeFixture("registry.json", registryRecords());
    const records = parseRegistryJson(file, "registry.json");

    expect(records).toMatchObject([
      {
        hive: "NTUSER",
        key: "Software\\Microsoft\\Windows\\CurrentVersion\\Run",
        valueName: "Updater",
        valueData: "C:\\Users\\Public\\updater.exe"
      },
      {
        hive: "SYSTEM",
        key: "CurrentControlSet\\Services\\EvilUpdater",
        valueName: "ImagePath",
        valueData: "C:\\ProgramData\\Updater\\svc.exe"
      },
      {
        hive: "SOFTWARE",
        key: "Microsoft\\Windows NT\\CurrentVersion\\Schedule\\TaskCache\\Tasks\\{11111111-1111-1111-1111-111111111111}",
        valueName: "Path"
      },
      {
        hive: "SYSTEM",
        key: "CurrentControlSet\\Control\\Session Manager\\AppCompatCache",
        valueName: "AppCompatCache"
      }
    ]);

    expect(
      matchRegistryRunKey(claim("Updater persisted via Run key C:\\Users\\Public\\updater.exe"), records)
    ).toMatchObject([
      {
        artifact: "registry.json",
        locator:
          "registry:hive=NTUSER:key=Software\\Microsoft\\Windows\\CurrentVersion\\Run:value=Updater",
        supports: "registry-run-key"
      }
    ]);
    expect(
      matchRegistryService(claim("EvilUpdater service installed svc.exe"), records)
    ).toMatchObject([
      {
        artifact: "registry.json",
        locator:
          "registry:hive=SYSTEM:key=CurrentControlSet\\Services\\EvilUpdater:value=ImagePath",
        supports: "registry-service"
      }
    ]);
    expect(
      matchRegistryScheduledTask(
        claim("DailyUpdater scheduled task launches taskdrop.exe"),
        records
      )
    ).toMatchObject([
      {
        artifact: "registry.json",
        locator:
          "registry:hive=SOFTWARE:key=Microsoft\\Windows NT\\CurrentVersion\\Schedule\\TaskCache\\Tasks\\{11111111-1111-1111-1111-111111111111}:value=Path",
        supports: "scheduled-task"
      }
    ]);
    expect(
      matchRegistryShimCache(
        claim("evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe"),
        records
      )
    ).toMatchObject([
      {
        artifact: "registry.json",
        locator:
          "registry:hive=SYSTEM:key=CurrentControlSet\\Control\\Session Manager\\AppCompatCache:value=AppCompatCache",
        supports: "shimcache_indicator"
      }
    ]);
    expect(
      [
        ...matchRegistryRunKey(claim("Updater persisted via Run key"), records),
        ...matchRegistryService(claim("EvilUpdater service installed"), records),
        ...matchRegistryScheduledTask(claim("DailyUpdater scheduled task"), records),
        ...matchRegistryShimCache(claim("evil.exe executed"), records)
      ].every((ref) => /^sha256:[a-f0-9]{64}$/u.test(ref.hash))
    ).toBe(true);
  });

  it("links registry evidence through persistence and program execution dispatch", async () => {
    const caseDir = await mkdtemp(join(tmpdir(), "findevil-registry-linker-"));
    await writeFile(join(caseDir, "registry.json"), JSON.stringify(registryRecords()), "utf8");

    const persistence = linkEvidence(
      baseClaim({
        text: "EvilUpdater service installed svc.exe",
        type: "persistence",
        missingEvidence: ["registry-service"]
      }),
      caseDir
    );
    expect(persistence.evidenceRefs.map((ref) => ref.supports)).toEqual(["registry-service"]);
    expect(persistence.missingEvidence).toEqual([]);

    const execution = linkEvidence(
      baseClaim({
        text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe",
        type: "program_execution",
        missingEvidence: ["shimcache_indicator"]
      }),
      caseDir
    );
    expect(execution.evidenceRefs.map((ref) => ref.supports)).toEqual(["shimcache_indicator"]);
    expect(execution.missingEvidence).toEqual([]);
  });
});

async function writeFixture(name: string, records: readonly unknown[]): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "findevil-registry-linker-"));
  const file = join(directory, name);
  await writeFile(file, JSON.stringify(records), "utf8");
  return file;
}

function registryRecords(): unknown[] {
  return [
    {
      HiveType: "NTUSER",
      KeyPath: "Software\\Microsoft\\Windows\\CurrentVersion\\Run",
      ValueName: "Updater",
      ValueType: "RegSz",
      ValueData: "C:\\Users\\Public\\updater.exe",
      LastWriteTimestamp: "2026-05-30T09:00:00Z"
    },
    {
      HiveType: "SYSTEM",
      KeyPath: "CurrentControlSet\\Services\\EvilUpdater",
      ValueName: "ImagePath",
      ValueType: "RegExpandSz",
      ValueData: "C:\\ProgramData\\Updater\\svc.exe",
      LastWriteTimestamp: "2026-05-30T09:05:00Z"
    },
    {
      HiveType: "SOFTWARE",
      KeyPath:
        "Microsoft\\Windows NT\\CurrentVersion\\Schedule\\TaskCache\\Tasks\\{11111111-1111-1111-1111-111111111111}",
      ValueName: "Path",
      ValueType: "RegSz",
      ValueData: "\\DailyUpdater C:\\ProgramData\\Updater\\taskdrop.exe",
      LastWriteTimestamp: "2026-05-30T09:10:00Z"
    },
    {
      HiveType: "SYSTEM",
      KeyPath: "CurrentControlSet\\Control\\Session Manager\\AppCompatCache",
      ValueName: "AppCompatCache",
      ValueType: "RegBinary",
      ValueData: "C:\\Users\\Public\\Downloads\\evil.exe 2026-05-30T09:55:00Z",
      LastWriteTimestamp: "2026-05-30T09:55:00Z"
    }
  ];
}

function claim(text: string): Pick<Claim, "text"> {
  return { text };
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-registry",
    text: "registry claim",
    type: "persistence",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}
