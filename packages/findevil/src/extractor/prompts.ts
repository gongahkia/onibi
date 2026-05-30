export const claimExtractorToolName = "emit_claim_ledger";

export const claimLedgerJsonSchema = {
  type: "object",
  additionalProperties: false,
  required: ["id", "generatedAt", "claims"],
  properties: {
    id: { type: "string", minLength: 1 },
    runId: { type: "string", minLength: 1 },
    generatedAt: { type: "string", format: "date-time" },
    claims: {
      type: "array",
      items: {
        type: "object",
        additionalProperties: false,
        required: [
          "id",
          "text",
          "type",
          "severity",
          "status",
          "confidence",
          "evidenceRefs",
          "missingEvidence"
        ],
        properties: {
          id: { type: "string", minLength: 1 },
          text: { type: "string", minLength: 1 },
          type: {
            type: "string",
            enum: [
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
            ]
          },
          severity: {
            type: "string",
            enum: ["informational", "low", "medium", "high", "critical"]
          },
          status: {
            type: "string",
            enum: ["confirmed", "inferred", "unsupported", "contradicted", "unverifiable"]
          },
          confidence: { type: "number", minimum: 0, maximum: 1 },
          evidenceRefs: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["artifact", "locator", "supports", "hash"],
              properties: {
                artifact: { type: "string", minLength: 1 },
                locator: { type: "string", minLength: 1 },
                supports: { type: "string", minLength: 1 },
                hash: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" }
              }
            }
          },
          missingEvidence: {
            type: "array",
            items: { type: "string", minLength: 1 }
          },
          repairAction: {
            type: "object",
            additionalProperties: false,
            required: ["kind", "prompt"],
            properties: {
              kind: { type: "string", enum: ["targeted_reanalysis", "safe_reanalysis"] },
              prompt: { type: "string", minLength: 1 }
            }
          },
          sourceLocator: { type: "string", minLength: 1 }
        }
      }
    }
  }
} as const;

export const claimExtractorSystemPrompt = [
  "You extract atomic DFIR claims from Protocol SIFT incident reports.",
  "Return only the tool call payload matching the provided JSON schema.",
  "Split compound findings into separate claims with stable claim-0001 style ids.",
  "Use status unverifiable unless the report itself includes direct proof and a valid sha256 evidence hash.",
  "Do not invent hashes, artifacts, or evidence locators.",
  "For high-impact claims, list missingEvidence for the proof needed to confirm the claim."
].join("\n");

export function buildClaimExtractorUserPrompt(report: string, validationError?: string): string {
  return [
    "Extract an audit-grade claim ledger from this Protocol SIFT report.",
    "",
    "Claim type guidance:",
    "- program_execution requires Prefetch, Amcache execution record, ShimCache indicator, or Sysmon process-create proof.",
    "- persistence requires a Run key, scheduled task, or service creation proof.",
    "- network_connection requires netflow or PCAP flow proof; DNS alone is only inferential.",
    "",
    "Set evidenceRefs to [] when the report cites an artifact without a valid sha256 row/content hash.",
    "Use missingEvidence to name the proof still needed.",
    validationError ? `\nPrevious response failed schema validation:\n${validationError}\n` : "",
    "Report:",
    report
  ].join("\n");
}
