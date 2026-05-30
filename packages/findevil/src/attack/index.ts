import { attackCatalog, isAttackTechniqueId, type AttackTechniqueId } from "./catalog.js";
import type { AttackTechnique, Claim, ClaimType } from "../types/claim.js";

export { attackCatalog, isAttackTechniqueId };
export type { AttackTechniqueId };

export const attackTechniqueIdsByClaimType = {
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
} as const satisfies Record<ClaimType, readonly AttackTechniqueId[]>;

export function suggestTechniquesForClaim(claim: Pick<Claim, "type">): AttackTechnique[] {
  return attackTechniqueIdsByClaimType[claim.type].map((id) => attackCatalog[id]);
}

export function resolveAttackTechniquesForClaim(
  claim: Pick<Claim, "type" | "attackTechniques">
): AttackTechnique[] {
  return (
    catalogTechniquesFromIds(claim.attackTechniques.map((technique) => technique.id)) ??
    suggestTechniquesForClaim(claim)
  );
}

export function catalogTechniquesFromIds(ids: readonly string[]): AttackTechnique[] | undefined {
  if (ids.length === 0 || ids.some((id) => !isAttackTechniqueId(id))) {
    return undefined;
  }
  return unique(ids).map((id) => attackCatalog[id]);
}

function unique(ids: readonly string[]): AttackTechniqueId[] {
  return [...new Set(ids)].filter(isAttackTechniqueId);
}
