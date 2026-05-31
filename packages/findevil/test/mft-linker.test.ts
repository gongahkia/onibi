import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  linkEvidence,
  matchMftFileCreate,
  matchMftFileDelete,
  matchMftFileModify,
  parseMftJson
} from "../src/linker/index.js";
import type { Claim } from "../src/types/claim.js";
import { verifyClaim } from "../src/verifier/index.js";

describe("MFT evidence linker", () => {
  it("parses MFTECmd JSON and links SI/FN file create, modify, and delete evidence", () => {
    const records = parseMftJson(inlineMftJson());

    expect(records).toMatchObject([
      {
        artifact: "mft.json",
        entryNumber: "42",
        fileName: "evil.exe",
        extension: "exe",
        parentPath: "C:\\Users\\Public\\Downloads",
        inUse: true,
        si: {
          created: "2026-05-30T09:58:00Z",
          modified: "2026-05-30T10:10:00Z"
        },
        fn: {
          created: "2026-05-30T09:58:01Z",
          modified: "2026-05-30T10:10:01Z"
        }
      },
      {
        entryNumber: "43",
        fileName: "old.dll",
        extension: "dll",
        inUse: false
      }
    ]);

    const create = matchMftFileCreate(
      { text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe" },
      records
    );
    expect(create.map((ref) => ref.locator)).toEqual([
      "mft:record=42:attr=SI",
      "mft:record=42:attr=FN"
    ]);
    expect(create.every((ref) => ref.supports === "mft-file-create")).toBe(true);
    expect(create.every((ref) => /^sha256:[a-f0-9]{64}$/u.test(ref.hash))).toBe(true);

    expect(
      matchMftFileModify({ text: "evil.exe was modified in Downloads" }, records).map(
        (ref) => ref.locator
      )
    ).toEqual(["mft:record=42:attr=SI", "mft:record=42:attr=FN"]);

    const deleted = matchMftFileDelete({ text: "old.dll was deleted" }, records);
    expect(deleted.map((ref) => ref.locator)).toEqual([
      "mft:record=43:attr=SI",
      "mft:record=43:attr=FN"
    ]);
    expect(deleted.every((ref) => ref.supports === "mft-file-delete")).toBe(true);
  });

  it("adds MFT create evidence without confirming program execution", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mft-"));
    await writeFile(join(directory, "mftecmd-mft.json"), inlineMftJson(), "utf8");

    const linked = linkEvidence(
      baseClaim({
        text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe",
        type: "program_execution",
        missingEvidence: []
      }),
      directory
    );

    expect(linked.evidenceRefs).toMatchObject([
      {
        artifact: "mftecmd-mft.json",
        locator: "mft:record=42:attr=SI",
        supports: "mft-file-create"
      },
      {
        artifact: "mftecmd-mft.json",
        locator: "mft:record=42:attr=FN",
        supports: "mft-file-create"
      }
    ]);
    expect(verifyClaim(linked)).toBe("unsupported");
    expect(linked.missingEvidence).toContain("prefetch_entry");
    expect(linked.missingEvidence).toContain("amcache_execution_record");
    expect(linked.missingEvidence).toContain("sysmon_process_create");
    expect(linked.missingEvidence).toContain("security_4688_process_create");
    expect(linked.missingEvidence).not.toContain("mft-file-create");
  });

  it("treats MFT create evidence as corroborating-only for persistence", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mft-"));
    await writeFile(join(directory, "mftecmd-mft.json"), inlineMftJson(), "utf8");

    const linked = linkEvidence(
      baseClaim({
        text: "evil.exe persisted from C:\\Users\\Public\\Downloads\\evil.exe",
        type: "persistence",
        missingEvidence: []
      }),
      directory
    );

    expect(linked.evidenceRefs.map((ref) => ref.supports)).toEqual([
      "mft-file-create",
      "mft-file-create"
    ]);
    expect(verifyClaim(linked)).toBe("inferred");
    expect(linked.missingEvidence).toContain("registry-run-key");
    expect(linked.missingEvidence).toContain("system_7045_service_create");
    expect(linked.missingEvidence).not.toContain("mft-file-create");
  });
});

function inlineMftJson(): string {
  return JSON.stringify({
    Records: [
      {
        EntryNumber: 42,
        InUse: true,
        ParentPath: "C:\\Users\\Public\\Downloads",
        FileName: "evil.exe",
        Extension: "exe",
        "SI Created": "2026-05-30T09:58:00Z",
        "SI Modified": "2026-05-30T10:10:00Z",
        "FN Created": "2026-05-30T09:58:01Z",
        "FN Modified": "2026-05-30T10:10:01Z"
      },
      {
        EntryNumber: 43,
        InUse: false,
        ParentPath: "C:\\Users\\Public\\Downloads",
        FileName: "old.dll",
        Extension: "dll",
        Created0x10: "2026-05-29T12:00:00Z",
        Modified0x10: "2026-05-29T12:05:00Z",
        Created0x30: "2026-05-29T12:00:01Z",
        Modified0x30: "2026-05-29T12:05:01Z"
      }
    ]
  });
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-mft",
    text: "evil.exe executed",
    type: "program_execution",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}
