import { createHash } from "node:crypto";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { linkEvidence } from "../src/linker/index.js";
import { matchPcapNetworkConnection, parseFlowSummaryJson } from "../src/linker/pcap.js";
import type { Claim } from "../src/types/claim.js";

describe("pcap flow-summary linker", () => {
  it("parses flow-summary JSON and matches destination domains", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-pcap-"));
    const file = join(directory, "flow-summary.json");
    await writeFile(file, JSON.stringify(flowSummaryFixture()), "utf8");

    const flows = parseFlowSummaryJson(file);
    expect(flows).toMatchObject([
      {
        srcIp: "10.10.5.23",
        srcPort: 49712,
        destIp: "198.51.100.44",
        destPort: 443,
        destDomain: "c2.example.test",
        protocol: "tcp"
      }
    ]);

    const refs = matchPcapNetworkConnection(
      baseClaim({ text: "powershell.exe connected to c2.example.test over TCP/443" }),
      flows
    );

    expect(refs).toHaveLength(1);
    expect(refs[0]).toMatchObject({
      artifact: file,
      locator: `pcap:flow=${sha256("10.10.5.23|49712|198.51.100.44|443|tcp")}`,
      supports: "netflow-or-pcap"
    });
    expect(refs[0]?.hash).toMatch(/^sha256:[a-f0-9]{64}$/u);
  });

  it("links PCAP evidence into network claims by destination IP", async () => {
    const caseDir = await mkdtemp(join(tmpdir(), "findevil-pcap-case-"));
    const pcapDir = join(caseDir, "pcap");
    await mkdir(pcapDir);
    await writeFile(
      join(pcapDir, "flow-summary.json"),
      JSON.stringify(flowSummaryFixture()),
      "utf8"
    );

    const linked = linkEvidence(
      baseClaim({ text: "WIN-LAB01 established a connection to 198.51.100.44 over TCP/443" }),
      caseDir
    );

    expect(linked.evidenceRefs).toEqual([
      expect.objectContaining({
        artifact: "pcap/flow-summary.json",
        locator: expect.stringMatching(/^pcap:flow=[a-f0-9]{64}$/u),
        supports: "netflow-or-pcap"
      })
    ]);
    expect(linked.missingEvidence).toEqual([]);
  });

  it("normalizes flat Zeek-style flow records", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-pcap-zeek-"));
    const file = join(directory, "conn.log.json");
    await writeFile(
      file,
      JSON.stringify([
        {
          uid: "Cv2Qf84",
          "id.orig_h": "10.10.5.23",
          "id.orig_p": 49712,
          "id.resp_h": "198.51.100.44",
          "id.resp_p": "443",
          proto: "tcp",
          server_name: "c2.example.test"
        }
      ]),
      "utf8"
    );

    const refs = matchPcapNetworkConnection(
      baseClaim({ text: "powershell.exe connected to c2.example.test" }),
      parseFlowSummaryJson(file)
    );

    expect(refs).toHaveLength(1);
    expect(refs[0]?.locator).toBe(`pcap:flow=${sha256("10.10.5.23|49712|198.51.100.44|443|tcp")}`);
  });
});

function flowSummaryFixture(): unknown {
  return {
    sourceType: "pcap_flow_summary",
    flows: [
      {
        flowId: "flow-20260218-094001-001",
        process: "powershell.exe",
        user: "LAB/analyst01",
        srcIp: "10.10.5.23",
        srcPort: 49712,
        destHost: "c2.example.test",
        destIp: "198.51.100.44",
        destPort: 443,
        protocol: "tcp/tls",
        startTime: "2026-02-18T09:40:01.000Z",
        endTime: "2026-02-18T09:40:17.000Z",
        bytesOut: 18432,
        bytesIn: 2208
      }
    ]
  };
}

function baseClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-pcap",
    text: "WIN-LAB01 established a network connection",
    type: "network_connection",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: ["netflow-or-pcap"],
    ...overrides
  };
}

function sha256(input: string): string {
  return createHash("sha256").update(input).digest("hex");
}
