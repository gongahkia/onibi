import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  linkEvidence,
  matchVolatilityCmdline,
  matchVolatilityMalfind,
  matchVolatilityNetscan,
  matchVolatilityPslist,
  parseVolatilityJson
} from "../src/linker/index.js";
import { verifyClaim } from "../src/verifier/index.js";
import type { Claim } from "../src/types/claim.js";

describe("Volatility memory linker", () => {
  it("parses Volatility 3 JSON rows and emits plugin row locators", () => {
    const records = parseVolatilityJson(
      JSON.stringify({
        plugin: "windows.pslist.PsList",
        columns: [
          { name: "PID", type: "int" },
          { name: "ImageFileName", type: "str" }
        ],
        rows: [
          [4, "System"],
          [1337, "evil.exe"]
        ]
      })
    );

    expect(records).toMatchObject([
      {
        plugin: "windows.pslist",
        row: 1,
        sourceLocator: "volatility:plugin=windows.pslist:row=1",
        data: {
          PID: "4",
          ImageFileName: "System"
        }
      },
      {
        plugin: "windows.pslist",
        row: 2,
        sourceLocator: "volatility:plugin=windows.pslist:row=2",
        data: {
          PID: "1337",
          ImageFileName: "evil.exe"
        }
      }
    ]);
  });

  it("matches pslist, malfind, cmdline, and netscan records", () => {
    const records = [
      ...parseVolatilityJson(JSON.stringify(pslistFixture())),
      ...parseVolatilityJson(JSON.stringify(malfindFixture())),
      ...parseVolatilityJson(JSON.stringify(cmdlineFixture())),
      ...parseVolatilityJson(JSON.stringify(netscanFixture()))
    ];

    const programClaim = baseProgramClaim({
      text: "evil.exe executed from memory with --stage"
    });
    const networkClaim = baseNetworkClaim({
      text: "evil.exe connected to 203.0.113.55 over TCP/443"
    });

    expect(matchVolatilityPslist(programClaim, records)).toEqual([
      expect.objectContaining({
        artifact: "volatility.json",
        locator: "volatility:plugin=windows.pslist:row=2",
        supports: "volatility-pslist"
      })
    ]);
    expect(matchVolatilityMalfind(programClaim, records)).toEqual([
      expect.objectContaining({
        locator: "volatility:plugin=windows.malfind:row=1",
        supports: "volatility-malfind"
      })
    ]);
    expect(matchVolatilityCmdline(programClaim, records)).toEqual([
      expect.objectContaining({
        locator: "volatility:plugin=windows.cmdline:row=1",
        supports: "volatility-cmdline"
      })
    ]);
    expect(matchVolatilityNetscan(networkClaim, records)).toEqual([
      expect.objectContaining({
        locator: "volatility:plugin=windows.netscan:row=1",
        supports: "volatility-netscan"
      })
    ]);
  });

  it("links Volatility evidence into program execution and network claims", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-memory-"));
    await writeFile(
      join(directory, "windows.pslist.json"),
      JSON.stringify(pslistFixture()),
      "utf8"
    );
    await writeFile(
      join(directory, "windows.malfind.json"),
      JSON.stringify(malfindFixture()),
      "utf8"
    );
    await writeFile(
      join(directory, "windows.cmdline.json"),
      JSON.stringify(cmdlineFixture()),
      "utf8"
    );
    await writeFile(
      join(directory, "windows.netscan.json"),
      JSON.stringify(netscanFixture()),
      "utf8"
    );

    const program = linkEvidence(
      baseProgramClaim({ text: "evil.exe executed with injected memory" }),
      directory
    );
    const network = linkEvidence(
      baseNetworkClaim({ text: "evil.exe connected to 203.0.113.55:443" }),
      directory
    );

    expect(program.evidenceRefs).toEqual([
      expect.objectContaining({
        artifact: "windows.cmdline.json",
        supports: "volatility-cmdline"
      }),
      expect.objectContaining({
        artifact: "windows.malfind.json",
        supports: "volatility-malfind"
      }),
      expect.objectContaining({
        artifact: "windows.pslist.json",
        supports: "volatility-pslist"
      })
    ]);
    expect(program.missingEvidence).toEqual([]);
    expect(verifyClaim(program)).toBe("confirmed");
    expect(network.evidenceRefs).toEqual([
      expect.objectContaining({
        artifact: "windows.netscan.json",
        locator: "volatility:plugin=windows.netscan:row=1",
        supports: "volatility-netscan"
      })
    ]);
    expect(network.missingEvidence).toEqual([]);
    expect(verifyClaim(network)).toBe("confirmed");
  });
});

function pslistFixture(): unknown {
  return {
    plugin: "windows.pslist.PsList",
    rows: [
      {
        PID: 4,
        PPID: 0,
        ImageFileName: "System"
      },
      {
        PID: 1337,
        PPID: 512,
        ImageFileName: "evil.exe",
        CreateTime: "2026-05-30T10:00:00Z"
      }
    ]
  };
}

function malfindFixture(): unknown {
  return {
    plugin: "windows.malfind.Malfind",
    rows: [
      {
        PID: 1337,
        Process: "evil.exe",
        "Start VPN": "0x7ff600000000",
        Protection: "PAGE_EXECUTE_READWRITE",
        "File output": "Disabled"
      }
    ]
  };
}

function cmdlineFixture(): unknown {
  return {
    plugin: "windows.cmdline.CmdLine",
    rows: [
      {
        PID: 1337,
        Process: "evil.exe",
        Args: "C:\\Users\\Public\\evil.exe --stage"
      }
    ]
  };
}

function netscanFixture(): unknown {
  return {
    plugin: "windows.netscan.NetScan",
    rows: [
      {
        Proto: "TCPv4",
        LocalAddr: "10.0.0.5",
        LocalPort: 49712,
        ForeignAddr: "203.0.113.55",
        ForeignPort: 443,
        State: "ESTABLISHED",
        PID: 1337,
        Owner: "evil.exe"
      }
    ]
  };
}

function baseProgramClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-memory-program",
    text: "evil.exe executed",
    type: "program_execution",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: ["volatility-pslist", "volatility-malfind"],
    ...overrides
  };
}

function baseNetworkClaim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-memory-network",
    text: "evil.exe established a network connection",
    type: "network_connection",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: ["volatility-netscan"],
    ...overrides
  };
}
