import { appendFile, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  addEvidenceFile,
  compareEvidenceWorkspaces,
  createEvidenceWorkspace,
  evidenceWorkspaceSummary,
  importBurpEvidence,
  importNessusEvidence,
  importNmapEvidence,
  importNucleiEvidence,
  importSarifEvidence,
  importZapEvidence,
  loadEvidenceWorkspace,
  qaEvidenceWorkspace,
  renderEvidenceWorkspaceHtml,
  signEvidenceWorkspace,
  verifyEvidenceWorkspace
} from "../src/index.js";

describe("KelpClaw evidence workspace", () => {
  it("preserves evidence, imports SARIF findings, signs, verifies, and catches tampering", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-"));
    const notePath = join(tempDir, "operator-note.txt");
    const sarifPath = join(tempDir, "findings.sarif");
    await writeFile(notePath, "operator observed auth bypass\n", "utf8");
    await writeFile(sarifPath, `${JSON.stringify(sarifFixture("warning"), null, 2)}\n`, "utf8");

    try {
      const workspace = await createEvidenceWorkspace(tempDir, {
        client: "Example Client",
        project: "Agent Governance Review",
        scope: ["repo:kelp-claw"]
      });
      expect(workspace.workspace.engagement).toMatchObject({
        client: "Example Client",
        project: "Agent Governance Review",
        scope: ["repo:kelp-claw"]
      });

      const added = await addEvidenceFile(tempDir, {
        filePath: notePath,
        kind: "note",
        title: "Operator note",
        sensitivity: "internal",
        tags: ["operator"]
      });
      expect(added.record).toMatchObject({
        kind: "note",
        title: "Operator note",
        sensitivity: "internal"
      });

      const imported = await importSarifEvidence(tempDir, sarifPath);
      expect(imported).toMatchObject({
        importedFindings: 1,
        metadata: { format: "sarif", validRecords: 1 }
      });

      const signed = await signEvidenceWorkspace(tempDir);
      expect(signed).toMatchObject({
        signaturePath: expect.stringContaining(".sig"),
        publicKeyPath: expect.stringContaining(".pub.json"),
        keyId: expect.stringMatching(/^sha256:/u)
      });
      expect(signed.manifest.artifacts.map((artifact) => artifact.path)).toEqual(
        expect.arrayContaining([
          "workspace.json",
          "evidence/index.json",
          "normalized/findings.json",
          "audit-log.jsonl"
        ])
      );
      await expect(verifyEvidenceWorkspace(tempDir)).resolves.toMatchObject({
        ok: true,
        manifestId: signed.manifest.manifestId,
        signature: {
          signed: true,
          valid: true,
          algorithm: "ed25519",
          keyId: signed.keyId
        },
        failures: []
      });
      await expect(evidenceWorkspaceSummary(tempDir)).resolves.toMatchObject({
        evidenceCount: 1,
        findingCount: 1,
        signed: true,
        verified: true,
        sourceReferenceGaps: 0
      });

      await appendFile(join(tempDir, "normalized", "findings.json"), "\n", "utf8");
      await expect(verifyEvidenceWorkspace(tempDir)).resolves.toMatchObject({
        ok: false,
        signature: { signed: true, valid: true },
        failures: expect.arrayContaining([
          expect.objectContaining({
            path: "normalized/findings.json",
            message: "covered file digest mismatch"
          })
        ])
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("imports passive scanner outputs into normalized evidence findings", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-scanners-"));
    const nmapPath = join(tempDir, "nmap.xml");
    const nucleiPath = join(tempDir, "nuclei.jsonl");
    const burpPath = join(tempDir, "burp.xml");
    const zapPath = join(tempDir, "zap.json");
    const nessusPath = join(tempDir, "nessus.xml");
    await writeFile(nmapPath, nmapFixture(), "utf8");
    await writeFile(nucleiPath, `${JSON.stringify(nucleiFixture())}\n`, "utf8");
    await writeFile(burpPath, burpFixture(), "utf8");
    await writeFile(zapPath, `${JSON.stringify(zapFixture(), null, 2)}\n`, "utf8");
    await writeFile(nessusPath, nessusFixture(), "utf8");

    try {
      await createEvidenceWorkspace(tempDir);
      await expect(importNmapEvidence(tempDir, nmapPath)).resolves.toMatchObject({
        importedFindings: 1,
        metadata: { format: "nmap" }
      });
      await expect(importNucleiEvidence(tempDir, nucleiPath)).resolves.toMatchObject({
        importedFindings: 1,
        metadata: { format: "nuclei" }
      });
      await expect(importBurpEvidence(tempDir, burpPath)).resolves.toMatchObject({
        importedFindings: 1,
        metadata: { format: "burp" }
      });
      await expect(importZapEvidence(tempDir, zapPath)).resolves.toMatchObject({
        importedFindings: 1,
        metadata: { format: "zap" }
      });
      await expect(importNessusEvidence(tempDir, nessusPath)).resolves.toMatchObject({
        importedFindings: 1,
        metadata: { format: "nessus" }
      });

      const summary = await evidenceWorkspaceSummary(tempDir);
      expect(summary.findingCount).toBe(5);
      const html = await renderEvidenceWorkspaceHtml(tempDir);
      expect(html).toContain("KelpClaw Evidence Workspace");
      expect(html).toContain("Open tcp/443 on 203.0.113.10");
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("derives stable AppSec finding IDs across scanner reruns", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-stable-ids-"));
    const sarifA = join(tempDir, "scanner-a.sarif");
    const sarifB = join(tempDir, "scanner-b.sarif");
    const sarifOther = join(tempDir, "scanner-other.sarif");
    const nucleiA = join(tempDir, "nuclei-a.jsonl");
    const nucleiB = join(tempDir, "nuclei-b.jsonl");
    const nucleiOther = join(tempDir, "nuclei-other.jsonl");
    const zapA = join(tempDir, "zap-a.json");
    const zapB = join(tempDir, "zap-b.json");
    const zapOther = join(tempDir, "zap-other.json");

    try {
      await writeFile(sarifA, `${JSON.stringify(sarifFixture("warning"), null, 2)}\n`, "utf8");
      await writeFile(sarifB, `${JSON.stringify(sarifFixture("warning"), null, 2)}\n`, "utf8");
      await writeFile(
        sarifOther,
        `${JSON.stringify(sarifFixture("warning", { uri: "skills/other/SKILL.md" }), null, 2)}\n`,
        "utf8"
      );
      await writeFile(nucleiA, `${JSON.stringify(nucleiFixture())}\n`, "utf8");
      await writeFile(nucleiB, `${JSON.stringify(nucleiFixture())}\n`, "utf8");
      await writeFile(
        nucleiOther,
        `${JSON.stringify({ ...nucleiFixture(), "matched-at": "https://admin.example.test" })}\n`,
        "utf8"
      );
      await writeFile(zapA, `${JSON.stringify(zapFixture(), null, 2)}\n`, "utf8");
      await writeFile(zapB, `${JSON.stringify(zapFixture(), null, 2)}\n`, "utf8");
      await writeFile(
        zapOther,
        `${JSON.stringify(zapFixture({ uri: "https://app.example.test/settings" }), null, 2)}\n`,
        "utf8"
      );

      const sarifWorkspaceA = join(tempDir, "sarif-a-workspace");
      await importSarifEvidence(sarifWorkspaceA, sarifA);
      await importSarifEvidence(sarifWorkspaceA, sarifB);
      expect((await loadEvidenceWorkspace(sarifWorkspaceA)).findings.findings).toHaveLength(1);
      const sarifFindingA = await firstFinding(sarifWorkspaceA);
      const sarifWorkspaceB = join(tempDir, "sarif-b-workspace");
      await importSarifEvidence(sarifWorkspaceB, sarifB);
      const sarifFindingB = await firstFinding(sarifWorkspaceB);
      const sarifWorkspaceOther = join(tempDir, "sarif-other-workspace");
      await importSarifEvidence(sarifWorkspaceOther, sarifOther);
      const sarifFindingOther = await firstFinding(sarifWorkspaceOther);
      expect(sarifFindingA.id).toBe(sarifFindingB.id);
      expect(sarifFindingA.id).not.toBe(sarifFindingOther.id);
      expect(sarifFindingA.provenance).toMatchObject({ upstreamId: "KC001", ruleId: "KC001" });
      expect(sarifFindingA.sourceReferences[0]?.metadata).toMatchObject({
        upstreamId: "KC001",
        ruleId: "KC001"
      });
      expect(sarifFindingA.mappings).toMatchObject({
        cwe: ["CWE-693"],
        owaspAsvs: ["V5.1.4"],
        owaspTop10: ["A05:2021"],
        owaspLlmTop10: ["LLM01:2025"]
      });

      const nucleiWorkspaceA = join(tempDir, "nuclei-a-workspace");
      const nucleiWorkspaceB = join(tempDir, "nuclei-b-workspace");
      const nucleiWorkspaceOther = join(tempDir, "nuclei-other-workspace");
      await importNucleiEvidence(nucleiWorkspaceA, nucleiA);
      await importNucleiEvidence(nucleiWorkspaceA, nucleiB);
      await importNucleiEvidence(nucleiWorkspaceB, nucleiB);
      await importNucleiEvidence(nucleiWorkspaceOther, nucleiOther);
      expect((await loadEvidenceWorkspace(nucleiWorkspaceA)).findings.findings).toHaveLength(1);
      const nucleiFindingA = await firstFinding(nucleiWorkspaceA);
      const nucleiFindingB = await firstFinding(nucleiWorkspaceB);
      const nucleiFindingOther = await firstFinding(nucleiWorkspaceOther);
      expect(nucleiFindingA.id).toBe(nucleiFindingB.id);
      expect(nucleiFindingA.id).not.toBe(nucleiFindingOther.id);
      expect(nucleiFindingA.provenance).toMatchObject({
        upstreamId: "http-missing-security-headers",
        templateId: "http-missing-security-headers"
      });
      expect(nucleiFindingA.mappings).toMatchObject({
        cwe: ["CWE-693"],
        owaspAsvs: ["V5.1.4"],
        owaspTop10: ["A05:2021"],
        owaspLlmTop10: ["LLM01:2025"]
      });

      const zapWorkspaceA = join(tempDir, "zap-a-workspace");
      const zapWorkspaceB = join(tempDir, "zap-b-workspace");
      const zapWorkspaceOther = join(tempDir, "zap-other-workspace");
      await importZapEvidence(zapWorkspaceA, zapA);
      await importZapEvidence(zapWorkspaceA, zapB);
      await importZapEvidence(zapWorkspaceB, zapB);
      await importZapEvidence(zapWorkspaceOther, zapOther);
      expect((await loadEvidenceWorkspace(zapWorkspaceA)).findings.findings).toHaveLength(1);
      const zapFindingA = await firstFinding(zapWorkspaceA);
      const zapFindingB = await firstFinding(zapWorkspaceB);
      const zapFindingOther = await firstFinding(zapWorkspaceOther);
      expect(zapFindingA.id).toBe(zapFindingB.id);
      expect(zapFindingA.id).not.toBe(zapFindingOther.id);
      expect(zapFindingA.provenance).toMatchObject({ upstreamId: "10016", pluginId: "10016" });
      expect(zapFindingA.mappings).toMatchObject({
        cwe: ["CWE-693"],
        owaspAsvs: ["V5.1.4"],
        owaspTop10: ["A05:2021"],
        owaspLlmTop10: ["LLM01:2025"]
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("reports QA and retest lifecycle for evidence workspaces", async () => {
    const baseline = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-baseline-"));
    const current = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-current-"));
    const baselineSarif = join(baseline, "baseline.sarif");
    const currentSarif = join(current, "current.sarif");
    await writeFile(baselineSarif, `${JSON.stringify(sarifFixture("warning"), null, 2)}\n`, "utf8");
    await writeFile(currentSarif, `${JSON.stringify(sarifFixture("error"), null, 2)}\n`, "utf8");

    try {
      await createEvidenceWorkspace(baseline);
      await createEvidenceWorkspace(current);
      await importSarifEvidence(baseline, baselineSarif);
      await importSarifEvidence(current, currentSarif);
      const qa = await qaEvidenceWorkspace(current);
      expect(qa.valid).toBe(true);
      expect(qa.issues).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            code: "manifest-verification-gap"
          })
        ])
      );

      const retest = await compareEvidenceWorkspaces(baseline, current);
      expect(retest.summary.regressed).toBe(1);
      expect(retest.findings).toEqual([
        expect.objectContaining({
          status: "regressed",
          matchedBy: "id"
        })
      ]);
    } finally {
      await rm(baseline, { recursive: true, force: true });
      await rm(current, { recursive: true, force: true });
    }
  });

  it("keeps AppSec benchmark scanner parser output stable", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-benchmark-"));
    const benchmarkRoot = appsecBenchmarkRoot();
    const scanners = join(benchmarkRoot, "scanners");
    const expected = JSON.parse(
      await readFile(join(benchmarkRoot, "expected", "benchmark.json"), "utf8")
    );

    try {
      await createEvidenceWorkspace(tempDir);
      const imports = [
        await importSarifEvidence(tempDir, join(scanners, "sarif.sarif")),
        await importNucleiEvidence(tempDir, join(scanners, "nuclei.jsonl")),
        await importZapEvidence(tempDir, join(scanners, "zap.json")),
        await importNmapEvidence(tempDir, join(scanners, "nmap.xml")),
        await importBurpEvidence(tempDir, join(scanners, "burp.xml")),
        await importNessusEvidence(tempDir, join(scanners, "nessus.xml"))
      ];
      expect(imports.map((result) => result.metadata.format).sort()).toEqual(
        expected.expectedScannerFormats
      );
      expect(imports.reduce((sum, result) => sum + result.importedFindings, 0)).toBe(
        expected.expectedImportedFindings
      );

      const state = await loadEvidenceWorkspace(tempDir);
      const snapshot = state.findings.findings
        .map((finding) => ({
          title: finding.title,
          severity: finding.severity,
          asset: finding.asset,
          weaknessIds: finding.weaknessIds,
          mappings: finding.mappings,
          sourceTools: finding.sourceReferences.map((source) => source.tool).sort(),
          upstreamId: finding.provenance.upstreamId
        }))
        .sort((left, right) => left.title.localeCompare(right.title));
      expect(snapshot.map((finding) => finding.title)).toEqual(expected.expectedTitles);
      expect(snapshot).toMatchInlineSnapshot(`
        [
          {
            "asset": "context/app.py",
            "mappings": {
              "cwe": [
                "CWE-1392",
              ],
              "owaspAsvs": [
                "V2.1.1",
              ],
              "owaspLlmTop10": [],
              "owaspTop10": [
                "A07:2021",
              ],
            },
            "severity": "medium",
            "sourceTools": [
              "sarif",
            ],
            "title": "Default admin marker exposed",
            "upstreamId": "BENCH_DEFAULT_ADMIN",
            "weaknessIds": [
              "CWE-1392",
            ],
          },
          {
            "asset": "http://127.0.0.1:8080/login",
            "mappings": {
              "cwe": [
                "CWE-693",
              ],
              "owaspAsvs": [
                "V5.1.4",
              ],
              "owaspLlmTop10": [],
              "owaspTop10": [
                "A05:2021",
              ],
            },
            "severity": "medium",
            "sourceTools": [
              "zap",
            ],
            "title": "Missing security header",
            "upstreamId": "10016",
            "weaknessIds": [
              "CWE-693",
            ],
          },
          {
            "asset": "127.0.0.1",
            "mappings": {
              "cwe": [],
              "owaspAsvs": [],
              "owaspLlmTop10": [],
              "owaspTop10": [],
            },
            "severity": "info",
            "sourceTools": [
              "nmap",
            ],
            "title": "Open tcp/8080 on 127.0.0.1",
            "upstreamId": "open-port",
            "weaknessIds": [],
          },
          {
            "asset": "http://127.0.0.1:8080",
            "mappings": {
              "cwe": [],
              "owaspAsvs": [],
              "owaspLlmTop10": [],
              "owaspTop10": [],
            },
            "severity": "high",
            "sourceTools": [
              "burp",
            ],
            "title": "Reflected test marker",
            "upstreamId": "1049088",
            "weaknessIds": [],
          },
          {
            "asset": "127.0.0.1",
            "mappings": {
              "cwe": [
                "CWE-327",
              ],
              "owaspAsvs": [],
              "owaspLlmTop10": [],
              "owaspTop10": [],
            },
            "severity": "medium",
            "sourceTools": [
              "nessus",
            ],
            "title": "SSH Protocol Versions Supported",
            "upstreamId": "10881",
            "weaknessIds": [
              "CWE-327",
            ],
          },
          {
            "asset": "http://127.0.0.1:8080/debug/env",
            "mappings": {
              "cwe": [
                "CWE-200",
              ],
              "owaspAsvs": [
                "V14.2.1",
              ],
              "owaspLlmTop10": [],
              "owaspTop10": [
                "A05:2021",
              ],
            },
            "severity": "high",
            "sourceTools": [
              "nuclei",
            ],
            "title": "Unauthenticated debug endpoint",
            "upstreamId": "bench-debug-env",
            "weaknessIds": [
              "CWE-200",
            ],
          },
        ]
      `);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });
});

async function firstFinding(root: string) {
  const state = await loadEvidenceWorkspace(root);
  const [finding] = state.findings.findings;
  expect(finding).toBeDefined();
  return finding!;
}

function sarifFixture(
  level: "warning" | "error",
  location: { readonly uri?: string; readonly startLine?: number } = {}
) {
  return {
    version: "2.1.0",
    runs: [
      {
        tool: {
          driver: {
            name: "KelpClaw Test Scanner",
            rules: [
              {
                id: "KC001",
                name: "Unsafe agent action",
                fullDescription: { text: "Agent action needs review." },
                help: { text: "Add policy enforcement." },
                properties: { tags: ["CWE-693", "ASVS-V5.1.4", "A05:2021", "LLM01:2025"] }
              }
            ]
          }
        },
        results: [
          {
            ruleId: "KC001",
            level,
            message: { text: "Unsafe action observed" },
            locations: [
              {
                physicalLocation: {
                  artifactLocation: { uri: location.uri ?? "skills/demo/SKILL.md" },
                  region: { startLine: location.startLine ?? 7 }
                }
              }
            ]
          }
        ]
      }
    ]
  };
}

function nmapFixture(): string {
  return `<?xml version="1.0"?>
<nmaprun>
  <host>
    <address addr="203.0.113.10" addrtype="ipv4"/>
    <ports>
      <port protocol="tcp" portid="443">
        <state state="open"/>
        <service name="https" product="nginx" version="1.25"/>
      </port>
    </ports>
  </host>
</nmaprun>
`;
}

function nucleiFixture() {
  return {
    "template-id": "http-missing-security-headers",
    "matched-at": "https://app.example.test",
    "matcher-name": "header",
    type: "http",
    info: {
      name: "Missing security header",
      severity: "medium",
      description: "The response is missing a security header.",
      remediation: "Set the missing header.",
      tags: "http,headers",
      classification: {
        "cwe-id": "CWE-693",
        "owasp-asvs": "V5.1.4",
        "owasp-top-ten": "A05:2021",
        "owasp-llm-top-ten": "LLM01:2025"
      },
      reference: ["https://example.test/header-hardening"]
    }
  };
}

function burpFixture(): string {
  return `<?xml version="1.0"?>
<issues>
  <issue>
    <type>1049088</type>
    <name>Cross-site scripting</name>
    <host>https://app.example.test</host>
    <path>/search</path>
    <severity>High</severity>
    <issueBackground>Reflected input is rendered unsafely.</issueBackground>
    <remediationBackground>Encode untrusted output.</remediationBackground>
  </issue>
</issues>
`;
}

function zapFixture(instance: { readonly uri?: string; readonly param?: string } = {}) {
  return {
    site: [
      {
        name: "https://app.example.test",
        alerts: [
          {
            pluginid: "10016",
            alertRef: "10016",
            name: "Web Browser XSS Protection Not Enabled",
            riskdesc: "Low (Medium)",
            desc: "The response does not enable browser XSS protections.",
            solution: "Set defensive response headers.",
            cweid: "CWE-693",
            owaspAsvs: ["V5.1.4"],
            owaspTop10: "A05:2021",
            owaspLlmTop10: ["LLM01:2025"],
            reference: "https://example.test/zap-reference",
            instances: [
              {
                uri: instance.uri ?? "https://app.example.test/login",
                param: instance.param ?? "X-XSS-Protection"
              }
            ]
          }
        ]
      }
    ]
  };
}

function nessusFixture(): string {
  return `<?xml version="1.0"?>
<NessusClientData_v2>
  <Report name="example">
    <ReportHost name="203.0.113.20">
      <ReportItem port="22" protocol="tcp" severity="2" pluginID="10881" pluginName="SSH Protocol Versions Supported">
        <description>The SSH service supports a legacy protocol configuration.</description>
        <solution>Disable legacy protocol support.</solution>
        <cwe>CWE-327</cwe>
        <see_also>https://example.test/ssh-hardening</see_also>
      </ReportItem>
    </ReportHost>
  </Report>
</NessusClientData_v2>
`;
}

function appsecBenchmarkRoot(): string {
  return fileURLToPath(
    new URL("../../../fixtures/appsec-benchmarks/multi-scanner", import.meta.url)
  );
}
