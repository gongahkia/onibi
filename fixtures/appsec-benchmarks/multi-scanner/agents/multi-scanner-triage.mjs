#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";

const input = JSON.parse(readFileSync(process.env.KELPCLAW_APPSEC_INPUT, "utf8"));
const findings = Array.isArray(input.findings) ? input.findings : [];
const merged = mergeMappings(findings);

writeFileSync(
  process.env.KELPCLAW_APPSEC_OUTPUT,
  `${JSON.stringify(
    {
      summary: `Benchmark triage correlated ${findings.length} scanner finding(s).`,
      triageFindings: [
        {
          id: "benchmark-multi-scanner",
          title: "Correlated benchmark scanner findings",
          severity: findings.some((finding) => finding.severity === "high") ? "high" : "medium",
          confidence: "medium",
          evidenceIds: findings.map((finding) => String(finding.id)).sort(),
          mappings: merged,
          rationale:
            "Offline benchmark agent groups every imported scanner finding into one triage item.",
          recommendedAction:
            "Review grouped evidence, assign owners, and rerun the offline benchmark after parser changes."
        }
      ],
      recommendedNextSteps: [
        "Compare normalized findings, generated SARIF, run metadata, and bundle manifest verification."
      ],
      limitations: [
        "Offline deterministic benchmark; no Docker daemon, target traffic, or live scanner execution."
      ]
    },
    null,
    2
  )}\n`
);

function mergeMappings(findings) {
  const merged = {
    cwe: [],
    owaspAsvs: [],
    owaspTop10: [],
    owaspLlmTop10: []
  };
  for (const finding of findings) {
    const mappings =
      finding.mappings && typeof finding.mappings === "object" ? finding.mappings : {};
    merged.cwe.push(...array(mappings.cwe ?? finding.weaknessIds));
    merged.owaspAsvs.push(...array(mappings.owaspAsvs));
    merged.owaspTop10.push(...array(mappings.owaspTop10));
    merged.owaspLlmTop10.push(...array(mappings.owaspLlmTop10));
  }
  return Object.fromEntries(
    Object.entries(merged).map(([key, values]) => [key, [...new Set(values)].sort()])
  );
}

function array(value) {
  return Array.isArray(value) ? value.map(String) : [];
}
