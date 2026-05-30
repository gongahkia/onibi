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

export const evidenceRefSchema = z.object({
  artifact: z.string().min(1),
  locator: z.string().min(1),
  supports: z.string().min(1),
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
export type EvidenceRef = z.infer<typeof evidenceRefSchema>;
export type RepairAction = z.infer<typeof repairActionSchema>;
export type Claim = z.infer<typeof claimSchema>;
export type ClaimLedger = z.infer<typeof claimLedgerSchema>;

export const placeholderEvidenceHash = `sha256:${"0".repeat(64)}`;

// TODO: phase 2A replace placeholder fixtures with extracted Protocol SIFT claims.
export const placeholderClaim: Claim = claimSchema.parse({
  id: "claim-0000",
  text: "placeholder claim awaiting Protocol SIFT report extraction",
  type: "program_execution",
  severity: "low",
  status: "unverifiable",
  confidence: 0,
  evidenceRefs: [
    {
      artifact: "placeholder.txt",
      locator: "line:1",
      supports: "placeholder",
      hash: placeholderEvidenceHash
    }
  ],
  missingEvidence: ["prefetch_entry"],
  repairAction: {
    kind: "targeted_reanalysis",
    prompt: "Re-run targeted analysis for this placeholder claim."
  }
});

export const emptyClaimLedger: ClaimLedger = claimLedgerSchema.parse({
  id: "claim-ledger-placeholder",
  generatedAt: "1970-01-01T00:00:00.000Z",
  claims: []
});
