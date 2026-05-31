import { mkdtemp } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { extractClaims, type ClaimExtractionAttempt } from "../src/extractor/index.js";

const tinyReport = [
  "# Protocol SIFT report",
  "Finding: evil.exe executed from C:\\Users\\Public\\Downloads.",
  "The report cites timeline presence but no Prefetch, Amcache, ShimCache, or Sysmon proof."
].join("\n");

const validLedger = {
  id: "claim-ledger-test",
  generatedAt: "2026-05-30T00:00:00.000Z",
  claims: [
    {
      id: "claim-0001",
      text: "evil.exe executed from C:\\Users\\Public\\Downloads.",
      type: "program_execution",
      severity: "high",
      status: "unverifiable",
      confidence: 0.41,
      evidenceRefs: [],
      missingEvidence: [
        "prefetch_entry",
        "amcache_execution_record",
        "shimcache_indicator",
        "sysmon_process_create"
      ],
      sourceLocator: "line:2"
    }
  ]
};

describe("extractClaims", () => {
  it("extracts and caches a schema-valid claim ledger", async () => {
    const cacheDir = await mkdtemp(join(tmpdir(), "findevil-extractor-"));
    let calls = 0;
    const ledger = await extractClaims(tinyReport, {
      cacheDir,
      complete: async () => {
        calls += 1;
        return {
          content: [
            {
              type: "tool_use",
              name: "emit_claim_ledger",
              input: validLedger
            }
          ]
        };
      }
    });

    expect(ledger).toMatchInlineSnapshot(`
      {
        "claims": [
          {
            "attackTechniques": [
              {
                "id": "T1059",
                "name": "Command and Scripting Interpreter",
                "tactic": "execution",
              },
            ],
            "confidence": 0.41,
            "evidenceRefs": [],
            "id": "claim-0001",
            "missingEvidence": [
              "prefetch_entry",
              "amcache_execution_record",
              "shimcache_indicator",
              "sysmon_process_create",
            ],
            "severity": "high",
            "sourceLocator": "line:2",
            "status": "unverifiable",
            "text": "evil.exe executed from C:\\Users\\Public\\Downloads.",
            "type": "program_execution",
          },
        ],
        "generatedAt": "2026-05-30T00:00:00.000Z",
        "id": "claim-ledger-test",
      }
    `);

    const cached = await extractClaims(tinyReport, {
      cacheDir,
      complete: async () => {
        throw new Error("cache miss");
      }
    });
    expect(cached).toEqual(ledger);
    expect(calls).toBe(1);
  });

  it("retries parse failures with the validation error appended", async () => {
    const cacheDir = await mkdtemp(join(tmpdir(), "findevil-extractor-retry-"));
    const attempts: ClaimExtractionAttempt[] = [];
    const ledger = await extractClaims(tinyReport, {
      cacheDir,
      maxRetries: 3,
      complete: async (attempt) => {
        attempts.push(attempt);
        return attempt.attempt === 0 ? { id: "invalid" } : validLedger;
      }
    });

    expect(ledger.id).toBe("claim-ledger-test");
    expect(attempts).toHaveLength(2);
    expect(attempts[1]?.validationError).toContain("generatedAt");
    expect(attempts[1]?.userPrompt).toContain("Previous response failed schema validation");
  });
});
