import { mkdtemp, readFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import { generateRepairPrompt } from "../src/repair/index.js";
import { runRepairLoop, type RepairAgentRunner } from "../src/repair/loop.js";
import type { Claim, ClaimLedger } from "../src/types/claim.js";

describe("repair loop", () => {
  it("targets unsupported high-severity claims and respects maxIterations", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-repair-"));
    const tracePath = join(directory, "repair-trace.jsonl");
    let calls = 0;
    const runner: RepairAgentRunner = async ({ claim, iteration, prompt }) => {
      calls += 1;
      return {
        output: `iteration ${iteration}`,
        events: [
          {
            session_id: "repair-session",
            hook_event_name: "PostToolUse",
            tool_name: "ClaudeCode",
            tool_input: { prompt },
            tool_response: { claimId: claim.id, iteration }
          }
        ]
      };
    };

    const result = await runRepairLoop(ledger([unsupportedProgramExecution()]), 2, {
      runner,
      tracePath,
      now: () => "2026-05-30T00:00:00.000Z"
    });

    expect(calls).toBe(2);
    expect(result.ledger.claims[0]?.status).toBe("unsupported");
    expect(result.trace.filter((row) => row.event === "repair_prompt")).toHaveLength(2);
    expect(result.trace.filter((row) => row.event === "agent_event")).toHaveLength(2);
    expect(await readFile(tracePath, "utf8")).toContain('"repair_result"');
  });

  it("accepts a repaired claim only after verifier rules pass", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-repair-verified-"));
    const runner: RepairAgentRunner = async ({ claim }) => ({
      claim: {
        ...claim,
        evidenceRefs: [
          {
            artifact: "prefetch.txt",
            locator: "line:1",
            supports: "prefetch_entry",
            hash: `sha256:${"b".repeat(64)}`
          }
        ]
      }
    });

    const result = await runRepairLoop(ledger([unsupportedProgramExecution()]), 3, {
      runner,
      tracePath: join(directory, "repair-trace.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z"
    });

    expect(result.ledger.claims[0]?.status).toBe("confirmed");
  });

  it("generates targeted repair prompts", () => {
    const repair = generateRepairPrompt(unsupportedProgramExecution());

    expect(repair.targetTools).toContain("Prefetch");
    expect(repair.prompt).toContain("Prove, retract, or downgrade claim claim-0001");
  });
});

function ledger(claims: readonly Claim[]): ClaimLedger {
  return {
    id: "claim-ledger-repair-test",
    generatedAt: "2026-05-30T00:00:00.000Z",
    claims: [...claims]
  };
}

function unsupportedProgramExecution(): Claim {
  return {
    id: "claim-0001",
    text: "evil.exe executed from C:\\Users\\Public\\Downloads\\evil.exe",
    type: "program_execution",
    severity: "high",
    status: "unsupported",
    confidence: 0.4,
    evidenceRefs: [
      {
        artifact: "timeline.csv",
        locator: "row:2",
        supports: "file_present",
        hash: `sha256:${"a".repeat(64)}`
      }
    ],
    missingEvidence: ["prefetch_entry"]
  };
}
