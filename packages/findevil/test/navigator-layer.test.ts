import { describe, expect, it } from "vitest";
import { buildNavigatorLayer } from "../src/attack/navigator-layer.js";
import { claimLedgerSchema } from "../src/types/claim.js";

describe("ATT&CK Navigator layer export", () => {
  it("snapshots technique coverage for a small ledger", () => {
    const ledger = claimLedgerSchema.parse({
      id: "claim-ledger-small",
      generatedAt: "2026-05-31T00:00:00.000Z",
      claims: [
        {
          id: "claim-001",
          text: "PowerShell executed an encoded command.",
          type: "program_execution",
          severity: "high",
          status: "confirmed",
          confidence: 0.9,
          attackTechniques: [
            {
              id: "T1059",
              name: "Command and Scripting Interpreter",
              tactic: "execution"
            }
          ],
          evidenceRefs: [],
          missingEvidence: []
        },
        {
          id: "claim-002",
          text: "Command shell execution remains unsupported.",
          type: "program_execution",
          severity: "medium",
          status: "unsupported",
          confidence: 0.4,
          attackTechniques: [
            {
              id: "T1059",
              name: "Command and Scripting Interpreter",
              tactic: "execution"
            }
          ],
          evidenceRefs: [],
          missingEvidence: []
        },
        {
          id: "claim-003",
          text: "HTTP beaconing was confirmed.",
          type: "network_connection",
          severity: "critical",
          status: "confirmed",
          confidence: 0.8,
          evidenceRefs: [],
          missingEvidence: []
        },
        {
          id: "claim-004",
          text: "Overall intrusion conclusion.",
          type: "incident_conclusion",
          severity: "informational",
          status: "confirmed",
          confidence: 0.7,
          evidenceRefs: [],
          missingEvidence: []
        }
      ]
    });

    expect(
      buildNavigatorLayer(ledger, {
        name: "Small ledger coverage",
        description: "Snapshot layer"
      })
    ).toMatchInlineSnapshot(`
      {
        "description": "Snapshot layer",
        "domain": "enterprise-attack",
        "gradient": {
          "colors": [
            "#d73027",
            "#fc8d59",
            "#fee08b",
            "#91cf60",
            "#1a9850",
          ],
          "maxValue": 1,
          "minValue": 0,
        },
        "name": "Small ledger coverage",
        "techniques": [
          {
            "color": "#fee08b",
            "comment": "Claim IDs: claim-001, claim-002",
            "score": 0.5,
            "techniqueID": "T1059",
          },
          {
            "color": "#1a9850",
            "comment": "Claim IDs: claim-003",
            "score": 1,
            "techniqueID": "T1071",
          },
        ],
        "versions": {
          "layer": "4.5",
          "navigator": "5.2.0",
        },
      }
    `);
  });
});
