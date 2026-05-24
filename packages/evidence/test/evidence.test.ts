import { appendFile, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
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
});

function sarifFixture(level: "warning" | "error") {
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
                properties: { tags: ["CWE-693"] }
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
                  artifactLocation: { uri: "skills/demo/SKILL.md" },
                  region: { startLine: 7 }
                }
              }
            ]
          }
        ]
      }
    ]
  };
}
