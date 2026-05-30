import { describe, expect, it } from "vitest";
import {
  attackCatalog,
  attackTechniqueIdsByClaimType,
  resolveAttackTechniquesForClaim,
  suggestTechniquesForClaim
} from "../src/attack/index.js";
import { attackTechniqueSchema, claimTypes, type ClaimType } from "../src/types/claim.js";

describe("MITRE ATT&CK claim mapping", () => {
  it("maps every claim type through a deterministic table", () => {
    expect(attackTechniqueIdsByClaimType).toEqual({
      file_presence: ["T1105"],
      program_execution: ["T1059"],
      persistence: ["T1547"],
      privilege_escalation: ["T1068"],
      credential_access: ["T1003"],
      network_connection: ["T1071"],
      lateral_movement: ["T1021"],
      data_exfiltration: ["T1041"],
      user_activity: ["T1078"],
      timeline_ordering: [],
      malware_identification: ["T1204"],
      incident_conclusion: []
    } satisfies Record<ClaimType, readonly string[]>);

    for (const type of claimTypes) {
      const first = suggestTechniquesForClaim({ type });
      const second = suggestTechniquesForClaim({ type });
      expect(second).toEqual(first);
      expect(first.map((technique) => technique.id)).toEqual(attackTechniqueIdsByClaimType[type]);
    }
  });

  it("validates all frozen catalog entries against the claim schema", () => {
    expect(Object.isFrozen(attackCatalog)).toBe(true);
    for (const technique of Object.values(attackCatalog)) {
      expect(Object.isFrozen(technique)).toBe(true);
      expect(attackTechniqueSchema.parse(technique)).toEqual(technique);
    }
  });

  it("keeps catalog-valid LLM IDs and falls back for unknown IDs", () => {
    expect(
      resolveAttackTechniquesForClaim({
        type: "program_execution",
        attackTechniques: [{ id: "T1071", name: "wrong", tactic: "wrong" }]
      })
    ).toEqual([attackCatalog.T1071]);

    expect(
      resolveAttackTechniquesForClaim({
        type: "program_execution",
        attackTechniques: [{ id: "T9999", name: "unknown", tactic: "unknown" }]
      })
    ).toEqual([attackCatalog.T1059]);
  });
});
