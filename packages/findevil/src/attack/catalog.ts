import type { AttackTechnique } from "../types/claim.js";

// Frozen subset of MITRE ATT&CK Enterprise techniques used by Find Evil claims.
// Source: https://attack.mitre.org/techniques/enterprise/
// Reference STIX repository: https://github.com/mitre/cti
export const attackCatalog = Object.freeze({
  T1059: Object.freeze({
    id: "T1059",
    name: "Command and Scripting Interpreter",
    tactic: "execution"
  }),
  T1547: Object.freeze({
    id: "T1547",
    name: "Boot or Logon Autostart Execution",
    tactic: "persistence"
  }),
  T1071: Object.freeze({
    id: "T1071",
    name: "Application Layer Protocol",
    tactic: "command-and-control"
  }),
  T1003: Object.freeze({
    id: "T1003",
    name: "OS Credential Dumping",
    tactic: "credential-access"
  }),
  T1021: Object.freeze({
    id: "T1021",
    name: "Remote Services",
    tactic: "lateral-movement"
  }),
  T1204: Object.freeze({
    id: "T1204",
    name: "User Execution",
    tactic: "execution"
  }),
  T1105: Object.freeze({
    id: "T1105",
    name: "Ingress Tool Transfer",
    tactic: "command-and-control"
  }),
  T1068: Object.freeze({
    id: "T1068",
    name: "Exploitation for Privilege Escalation",
    tactic: "privilege-escalation"
  }),
  T1041: Object.freeze({
    id: "T1041",
    name: "Exfiltration Over C2 Channel",
    tactic: "exfiltration"
  }),
  T1078: Object.freeze({
    id: "T1078",
    name: "Valid Accounts",
    tactic: "initial-access"
  })
} satisfies Record<string, AttackTechnique>);

export type AttackTechniqueId = keyof typeof attackCatalog;

export function isAttackTechniqueId(id: string): id is AttackTechniqueId {
  return id in attackCatalog;
}
