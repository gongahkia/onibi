import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  linkEvidence,
  matchYaraExecutionContext,
  matchYaraFamilyHit,
  parseYaraJson
} from "../src/linker/index.js";
import type { Claim } from "../src/types/claim.js";

describe("YARA evidence linker", () => {
  it("parses YARA JSON and links malware-identification claims by family or rule name", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-yara-"));
    const file = join(directory, "yara-results.json");
    await writeFile(file, JSON.stringify(yaraFixture()), "utf8");

    const matches = parseYaraJson(file);
    expect(matches).toMatchObject([
      {
        artifact: file,
        rule: "EvilClaw_Packed",
        target: "C:\\Users\\Public\\Downloads\\invoice_viewer.exe",
        namespace: "default",
        tags: ["malware"],
        meta: { family: "EvilClaw" },
        strings: [{ identifier: "$mz", offset: 0, length: 2, data: "4d5a" }]
      }
    ]);

    const refs = matchYaraFamilyHit(
      baseClaim({
        type: "malware_identification",
        text: "invoice_viewer.exe is the EvilClaw malware family"
      }),
      matches
    );

    expect(refs).toHaveLength(1);
    expect(refs[0]).toMatchObject({
      artifact: file,
      locator: "yara:rule=EvilClaw_Packed:target=C:\\Users\\Public\\Downloads\\invoice_viewer.exe",
      supports: "yara_hit"
    });
    expect(refs[0]?.hash).toMatch(/^sha256:[a-f0-9]{64}$/u);
  });

  it("links execution context when the program-execution claim mentions the YARA target", async () => {
    const matches = parseYaraJson(JSON.stringify(yaraFixture()));

    const refs = matchYaraExecutionContext(
      baseClaim({
        type: "program_execution",
        text: "invoice_viewer.exe executed from C:/Users/Public/Downloads/invoice_viewer.exe"
      }),
      matches
    );

    expect(refs).toHaveLength(1);
    expect(refs[0]).toMatchObject({
      artifact: "yara.json",
      locator: "yara:rule=EvilClaw_Packed:target=C:\\Users\\Public\\Downloads\\invoice_viewer.exe",
      supports: "yara_hit"
    });
  });

  it("dispatches YARA artifacts through linkEvidence and clears malware missing evidence", async () => {
    const caseDir = await mkdtemp(join(tmpdir(), "findevil-yara-case-"));
    const yaraDir = join(caseDir, "yara");
    await mkdir(yaraDir);
    await writeFile(join(yaraDir, "results.json"), JSON.stringify(yaraFixture()), "utf8");

    const linked = linkEvidence(
      baseClaim({
        type: "malware_identification",
        text: "invoice_viewer.exe is the EvilClaw malware family",
        missingEvidence: ["yara_hit"]
      }),
      caseDir
    );

    expect(linked.evidenceRefs).toEqual([
      expect.objectContaining({
        artifact: "yara/results.json",
        locator:
          "yara:rule=EvilClaw_Packed:target=C:\\Users\\Public\\Downloads\\invoice_viewer.exe",
        supports: "yara_hit"
      })
    ]);
    expect(linked.missingEvidence).toEqual([]);
  });
});

function yaraFixture(): unknown {
  return {
    scan: [
      {
        target: "C:\\Users\\Public\\Downloads\\invoice_viewer.exe",
        matches: [
          {
            rule: "EvilClaw_Packed",
            namespace: "default",
            tags: ["malware"],
            meta: {
              family: "EvilClaw",
              author: "IR lab"
            },
            strings: [
              {
                identifier: "$mz",
                matches: [{ offset: 0, length: 2, data: "4d5a" }]
              }
            ]
          }
        ]
      },
      {
        target: "C:\\Windows\\System32\\notepad.exe",
        matches: []
      }
    ]
  };
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-yara",
    text: "invoice_viewer.exe is malware",
    type: "malware_identification",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}
