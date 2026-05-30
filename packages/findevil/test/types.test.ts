import { describe, expect, it } from "vitest";
import {
  claimLedgerSchema,
  claimSchema,
  firewallEventSchema,
  placeholderClaim,
  placeholderFirewallEvent,
  placeholderTaintLedgerEntry,
  spoliationCheckSchema,
  taintLedgerEntrySchema
} from "../src/index.js";

describe("findevil schemas", () => {
  it("parses placeholder claim fixtures", () => {
    expect(claimSchema.parse(placeholderClaim)).toMatchObject({
      id: "claim-0000",
      type: "program_execution",
      status: "unverifiable"
    });
    expect(
      claimLedgerSchema.parse({
        id: "claim-ledger-test",
        generatedAt: "1970-01-01T00:00:00.000Z",
        claims: [placeholderClaim]
      })
    ).toMatchObject({
      claims: [expect.objectContaining({ id: "claim-0000" })]
    });
  });

  it("parses placeholder taint and firewall fixtures", () => {
    expect(taintLedgerEntrySchema.parse(placeholderTaintLedgerEntry)).toMatchObject({
      sensitivity: "case-data"
    });
    expect(firewallEventSchema.parse(placeholderFirewallEvent)).toMatchObject({
      eventType: "tainted_instruction_blocked",
      policyDecision: {
        action: "deny"
      }
    });
  });

  it("parses placeholder spoliation fixtures", () => {
    expect(
      spoliationCheckSchema.parse({
        id: "spoliation-check-test",
        root: "case-data",
        checkedAt: "1970-01-01T00:00:00.000Z",
        ok: true,
        before: [],
        after: [],
        added: [],
        removed: [],
        changed: []
      })
    ).toMatchObject({
      ok: true,
      root: "case-data"
    });
  });
});
