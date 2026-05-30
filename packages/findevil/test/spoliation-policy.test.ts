import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { evaluatePolicy } from "@kelpclaw/policy";
import { describe, expect, it } from "vitest";
import { createDfirSpoliationStrictPolicyPack } from "../../policy/src/packs/dfir-spoliation-strict.js";

describe("dfir spoliation policy pack", () => {
  it("denies evidence writes while allowing derived workspace writes", async () => {
    const caseRoot = await mkdtemp(join(tmpdir(), "kelpclaw-dfir-case-"));
    const evidenceRoot = join(caseRoot, "evidence");
    const derivedWorkspace = join(caseRoot, "derived");

    try {
      const pack = createDfirSpoliationStrictPolicyPack({
        evidenceRoot,
        derivedWorkspace
      });

      expect(
        evaluatePolicy(
          {
            tool: "Write",
            args: { path: join(evidenceRoot, "disk.E01") }
          },
          pack.ruleset
        )
      ).toMatchObject({
        action: "deny",
        matchedRuleIds: ["deny-write-into-evidence-root"]
      });

      expect(
        evaluatePolicy(
          {
            tool: "Bash",
            args: { command: `rm -rf ${join(evidenceRoot, "timeline.csv")}` }
          },
          pack.ruleset
        )
      ).toMatchObject({
        action: "deny",
        matchedRuleIds: ["deny-destructive-shell-in-evidence-root"]
      });

      expect(
        evaluatePolicy(
          {
            tool: "Write",
            args: { path: join(derivedWorkspace, "report.json") }
          },
          pack.ruleset
        )
      ).toMatchObject({
        action: "allow",
        matchedRuleIds: []
      });

      expect(
        evaluatePolicy(
          {
            tool: "Write",
            args: { path: join(caseRoot, "scratch", "report.json") }
          },
          pack.ruleset
        )
      ).toMatchObject({
        action: "require-approval",
        matchedRuleIds: ["warn-write-outside-derived-workspace"]
      });
    } finally {
      await rm(caseRoot, { recursive: true, force: true });
    }
  });
});
