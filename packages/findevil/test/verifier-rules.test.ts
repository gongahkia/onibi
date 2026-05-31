import { describe, expect, it } from "vitest";
import { verifyClaim } from "../src/verifier/index.js";
import type { Claim, EvidenceRef } from "../src/types/claim.js";

const hash = `sha256:${"a".repeat(64)}`;

describe("verifier rules", () => {
  it("confirms program execution only with direct execution evidence", () => {
    expect(
      verifyClaim(claim({ type: "program_execution", evidenceRefs: [evidence("prefetch_entry")] }))
    ).toBe("confirmed");
    expect(
      verifyClaim(claim({ type: "program_execution", evidenceRefs: [evidence("file_present")] }))
    ).toBe("unsupported");
  });

  it("confirms persistence with persistence artifacts and downgrades presence-only evidence", () => {
    expect(
      verifyClaim(claim({ type: "persistence", evidenceRefs: [evidence("registry-run-key")] }))
    ).toBe("confirmed");
    expect(
      verifyClaim(claim({ type: "persistence", evidenceRefs: [evidence("file_present")] }))
    ).toBe("inferred");
  });

  it("confirms network connections from flow evidence and treats DNS-only evidence as inferred", () => {
    expect(
      verifyClaim(
        claim({ type: "network_connection", evidenceRefs: [evidence("netflow-or-pcap")] })
      )
    ).toBe("confirmed");
    expect(
      verifyClaim(claim({ type: "network_connection", evidenceRefs: [evidence("dns_lookup")] }))
    ).toBe("inferred");
  });

  it("confirms malware identification with accepted YARA evidence", () => {
    expect(
      verifyClaim(claim({ type: "malware_identification", evidenceRefs: [evidence("yara_hit")] }))
    ).toBe("confirmed");
    expect(
      verifyClaim(claim({ type: "malware_identification", evidenceRefs: [evidence("yara_match")] }))
    ).toBe("inferred");
    expect(verifyClaim(claim({ type: "malware_identification", evidenceRefs: [] }))).toBe(
      "unverifiable"
    );
  });
});

function claim(overrides: Partial<Claim>): Claim {
  return {
    id: "claim-test",
    text: "test claim",
    type: "program_execution",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}

function evidence(supports: string): EvidenceRef {
  return {
    artifact: "artifact.txt",
    locator: "line:1",
    supports,
    hash
  };
}
