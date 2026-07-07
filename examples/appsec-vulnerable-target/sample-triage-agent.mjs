#!/usr/bin/env node
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";

const inputPath = process.env.KELPCLAW_APPSEC_INPUT;
const outputPath = process.env.KELPCLAW_APPSEC_OUTPUT;

if (!inputPath || !outputPath) {
  throw new Error("KELPCLAW_APPSEC_INPUT and KELPCLAW_APPSEC_OUTPUT are required.");
}

const input = JSON.parse(readFileSync(inputPath, "utf8"));
const findings = Array.isArray(input.findings) ? input.findings : [];

const triageFindings = findings.map((finding) => {
  const id = createHash("sha256").update(String(finding.id)).digest("hex").slice(0, 12);
  return {
    id: `sample-triage-${id}`,
    title: `Review ${finding.title}`,
    severity: severity(finding.severity),
    confidence: "medium",
    evidenceIds: [String(finding.id)],
    mappings: mappings(finding),
    rationale: `Sample agent correlated scanner evidence ${finding.id}.`,
    recommendedAction:
      "Remove the exposed fixture behavior or gate it behind authenticated local-only access, then rerun passive scanners."
  };
});

writeFileSync(
  outputPath,
  `${JSON.stringify(
    {
      summary: `Sample triage correlated ${triageFindings.length} scanner finding(s).`,
      triageFindings,
      recommendedNextSteps: [
        "Review the bundled SARIF and Nuclei fixtures.",
        "Replace the sample target with an owned local lab target before using live scanners."
      ],
      limitations: [
        "Deterministic sample agent only.",
        "No exploit execution, live network scanning, or internet access was performed."
      ]
    },
    null,
    2
  )}\n`
);

function severity(value) {
  return ["critical", "high", "medium", "low", "info"].includes(value) ? value : "info";
}

function mappings(finding) {
  const existing = finding.mappings && typeof finding.mappings === "object" ? finding.mappings : {};
  return {
    cwe: array(existing.cwe ?? finding.weaknessIds),
    owaspAsvs: array(existing.owaspAsvs),
    owaspTop10: array(existing.owaspTop10),
    owaspLlmTop10: array(existing.owaspLlmTop10)
  };
}

function array(value) {
  return Array.isArray(value) ? value.map(String) : [];
}
