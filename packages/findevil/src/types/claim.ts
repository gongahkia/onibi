import { z } from "zod";

export const claimTypes = [
  "file_presence",
  "program_execution",
  "persistence",
  "privilege_escalation",
  "credential_access",
  "network_connection",
  "lateral_movement",
  "data_exfiltration",
  "user_activity",
  "timeline_ordering",
  "malware_identification",
  "incident_conclusion"
] as const;

export const claimStatuses = [
  "confirmed",
  "inferred",
  "unsupported",
  "contradicted",
  "unverifiable"
] as const;

export const claimSeveritySchema = z.enum(["informational", "low", "medium", "high", "critical"]);
export const claimTypeSchema = z.enum(claimTypes);
export const claimStatusSchema = z.enum(claimStatuses);
export const evidenceSupportTypes = [
  "amcache_execution_record",
  "contradicts_installation",
  "file_present",
  "mft-file-create",
  "mft-file-delete",
  "mft-file-modify",
  "netflow-or-pcap",
  "pcap_flow_summary",
  "prefetch_entry",
  "registry-run-key",
  "registry-service",
  "scheduled-task",
  "security_4624_logon",
  "security_4625_logon",
  "security_4688_process_create",
  "security_4698_scheduled_task",
  "security_4702_scheduled_task",
  "service-create",
  "shimcache_indicator",
  "sigma_rule_match",
  "srum_network_activity",
  "sysmon_network_connect",
  "sysmon_process_create",
  "system_7045_service_create",
  "volatility-cmdline",
  "volatility-malfind",
  "volatility-netscan",
  "volatility-pslist",
  "yara_hit"
] as const;
export const evidenceSupportSchema = z.enum(evidenceSupportTypes).or(z.string().min(1));

export const attackTechniqueSchema = z.object({
  id: z.string().regex(/^T\d{4}(\.\d{3})?$/),
  name: z.string(),
  tactic: z.string()
});

export const evidenceRefSchema = z.object({
  artifact: z.string().min(1),
  locator: z.string().min(1),
  supports: evidenceSupportSchema,
  hash: z.string().regex(/^sha256:[a-f0-9]{64}$/)
});

export const repairActionSchema = z.object({
  kind: z.enum(["targeted_reanalysis", "safe_reanalysis"]),
  prompt: z.string().min(1)
});

export const claimSchema = z.object({
  id: z.string().min(1),
  text: z.string().min(1),
  type: claimTypeSchema,
  severity: claimSeveritySchema,
  status: claimStatusSchema,
  confidence: z.number().min(0).max(1),
  attackTechniques: z.array(attackTechniqueSchema).default([]),
  evidenceRefs: z.array(evidenceRefSchema),
  missingEvidence: z.array(z.string().min(1)),
  repairAction: repairActionSchema.optional(),
  sourceLocator: z.string().min(1).optional()
});

export const claimLedgerSchema = z.object({
  id: z.string().min(1),
  runId: z.string().min(1).optional(),
  generatedAt: z.string().datetime(),
  claims: z.array(claimSchema)
});

export type ClaimType = z.infer<typeof claimTypeSchema>;
export type ClaimStatus = z.infer<typeof claimStatusSchema>;
export type AttackTechnique = z.infer<typeof attackTechniqueSchema>;
export type EvidenceRef = z.infer<typeof evidenceRefSchema>;
export type RepairAction = z.infer<typeof repairActionSchema>;
export type Claim = z.infer<typeof claimSchema>;
export type ClaimLedger = z.infer<typeof claimLedgerSchema>;

export const emptyClaimLedger: ClaimLedger = claimLedgerSchema.parse({
  id: "claim-ledger-empty",
  generatedAt: "1970-01-01T00:00:00.000Z",
  claims: []
});
