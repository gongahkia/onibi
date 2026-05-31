import { describe, expect, it } from "vitest";
import { matchEventLogProcessCreate, type EventLogRecord } from "../src/linker/eventlog.js";
import type { Claim } from "../src/types/claim.js";
import {
  loadCuratedRuleset,
  matchEventLogAgainstSigma,
  parseSigmaRuleYaml,
  sigmaMatchesAsEvidence
} from "../src/sigma/index.js";

describe("Sigma event log matching", () => {
  it("matches an inline Sigma rule against an inline Event Log fixture", () => {
    const rule = parseSigmaRuleYaml(String.raw`title: Inline PowerShell Encoded Command
id: 4eb458ec-8f4a-412e-a8df-b47f3f4b7c01
status: test
level: high
logsource:
  product: windows
  service: powershell
detection:
  selection_basic:
    EventID: 4104
  selection_payload:
    ScriptBlockText|contains: ['FromBase64String', '-EncodedCommand']
  condition: selection_basic and selection_payload`);
    const records: EventLogRecord[] = [
      {
        artifact: "powershell.evtx.json",
        eventId: 4104,
        channel: "Microsoft-Windows-PowerShell/Operational",
        recordId: "4104",
        provider: "Microsoft-Windows-PowerShell",
        timeCreated: "2026-05-30T00:00:00.000Z",
        eventData: {
          ScriptBlockText:
            "powershell -NoP -EncodedCommand SQBFAFgA; [Convert]::FromBase64String($x)"
        },
        raw: {
          Event: {
            System: { EventID: 4104, Channel: "Microsoft-Windows-PowerShell/Operational" }
          }
        }
      }
    ];

    const matches = matchEventLogAgainstSigma(records, [rule]);
    expect(matches).toHaveLength(1);
    expect(matches[0]).toMatchObject({
      rule: { id: "4eb458ec-8f4a-412e-a8df-b47f3f4b7c01" },
      matchedSelections: ["selection_basic", "selection_payload"]
    });

    expect(sigmaMatchesAsEvidence(matches)).toMatchObject([
      {
        artifact: "powershell.evtx.json",
        locator:
          "evtx:channel=Microsoft-Windows-PowerShell/Operational:record=4104:sigma=4eb458ec-8f4a-412e-a8df-b47f3f4b7c01",
        supports: "sigma_rule_match"
      }
    ]);
    expect(sigmaMatchesAsEvidence(matches)[0]?.hash).toMatch(/^sha256:[a-f0-9]{64}$/u);
  });

  it("loads the curated deterministic Sigma ruleset", () => {
    const rules = loadCuratedRuleset();
    expect(rules.length).toBeGreaterThanOrEqual(10);
    expect(rules.length).toBeLessThanOrEqual(20);
    expect(rules.map((rule) => rule.id)).toContain("12ca9bd2-165c-41da-9a36-572ff6acdbf3");
  });

  it("merges Sigma evidence through the Event Log linker when requested", () => {
    const records: EventLogRecord[] = [
      {
        artifact: "security.evtx.json",
        eventId: 4688,
        channel: "Security",
        recordId: "4688",
        eventData: {
          NewProcessName: "C:\\Tools\\adfind.exe",
          CommandLine: "C:\\Tools\\adfind.exe -f objectcategory=computer"
        },
        raw: {
          Event: {
            System: { EventID: 4688, Channel: "Security" }
          }
        }
      }
    ];

    const refs = matchEventLogProcessCreate(
      { text: "adfind.exe executed", missingEvidence: ["sigma_rule_match"] } as Pick<Claim, "text">,
      records
    );
    expect(refs.map((ref) => ref.supports)).toEqual([
      "security_4688_process_create",
      "sigma_rule_match"
    ]);
    expect(refs[1]?.locator).toContain("sigma=02115df1-157b-47ec-91df-30fcc47e9cc0");
  });
});
