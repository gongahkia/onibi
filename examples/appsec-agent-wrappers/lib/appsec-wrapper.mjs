import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

export function readAppsecInput() {
  const inputPath = process.env.KELPCLAW_APPSEC_INPUT;
  if (!inputPath) throw new Error("KELPCLAW_APPSEC_INPUT is required.");
  return JSON.parse(readFileSync(inputPath, "utf8"));
}

export function writeAppsecOutput(output) {
  const outputPath = process.env.KELPCLAW_APPSEC_OUTPUT;
  if (!outputPath) throw new Error("KELPCLAW_APPSEC_OUTPUT is required.");
  writeFileSync(outputPath, `${JSON.stringify(validateTriageOutput(output), null, 2)}\n`);
}

export function deterministicTriage(input, label = "deterministic fixture") {
  const findings = Array.isArray(input.findings) ? input.findings : [];
  return {
    summary: `${label} correlated ${findings.length} scanner finding(s).`,
    triageFindings: findings.map((finding) => ({
      id: `wrapper-${hash(String(finding.id)).slice(0, 12)}`,
      title: `Review ${finding.title}`,
      severity: severity(finding.severity),
      confidence: "medium",
      evidenceIds: [String(finding.id)],
      mappings: mappings(finding),
      rationale: `${label} used only KelpClaw scanner evidence ${finding.id}.`,
      recommendedAction:
        "Assign an owner, remove the exposed behavior, and rerun passive scanner imports."
    })),
    recommendedNextSteps: ["Review evidence links before filing remediation work."],
    limitations: ["No exploit execution, live scanning, or target interaction was performed."]
  };
}

export function triagePrompt(input, label) {
  return `You are ${label}, an AppSec triage assistant.

Use only the KelpClaw input JSON below.
Do not execute exploits, persistence, lateral movement, or internet-wide scanning.
Do not claim validation unless KelpClaw supplied validation evidence.
Return only valid JSON with this shape:
${triageSchema()}

KelpClaw input JSON:
${JSON.stringify(input, null, 2)}
`;
}

export function runModelWrapper({ label, command, args, responsePath }) {
  const input = readAppsecInput();
  const prompt = triagePrompt(input, label);
  const result = spawnSync(command, args, {
    input: prompt,
    encoding: "utf8",
    env: { ...process.env, KELPCLAW_APPSEC_WRAPPER: label },
    maxBuffer: 32 * 1024 * 1024
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(`${label} exited ${result.status}: ${result.stderr || result.stdout}`);
  }
  const raw =
    responsePath && existsSync(responsePath) ? readFileSync(responsePath, "utf8") : result.stdout;
  writeAppsecOutput(parseTriageJson(raw));
}

export function tempResponsePath(prefix) {
  return join(tmpdir(), `${prefix}-${process.pid}-${Date.now()}.json`);
}

function parseTriageJson(raw) {
  const trimmed = raw.trim();
  const parsed = tryParseJson(trimmed);
  if (parsed) return unwrapModelJson(parsed);
  const fenced = /```(?:json)?\s*([\s\S]*?)```/iu.exec(trimmed)?.[1];
  if (fenced) {
    const fencedJson = tryParseJson(fenced.trim());
    if (fencedJson) return unwrapModelJson(fencedJson);
  }
  const objectText = trimmed.slice(trimmed.indexOf("{"), trimmed.lastIndexOf("}") + 1);
  const objectJson = objectText ? tryParseJson(objectText) : undefined;
  if (objectJson) return unwrapModelJson(objectJson);
  throw new Error("model output did not contain valid triage JSON.");
}

function unwrapModelJson(value) {
  if (hasTriageShape(value)) return value;
  for (const key of ["result", "response", "content", "message", "text"]) {
    const nested = value?.[key];
    if (typeof nested === "string") {
      const parsed = tryParseJson(nested.trim());
      if (parsed && hasTriageShape(parsed)) return parsed;
    }
  }
  return value;
}

function validateTriageOutput(value) {
  if (!hasTriageShape(value)) {
    throw new Error(
      "triage output requires summary, triageFindings, recommendedNextSteps, and limitations."
    );
  }
  return {
    summary: String(value.summary),
    triageFindings: value.triageFindings.map((finding, index) => ({
      id: String(finding.id ?? `wrapper-finding-${index + 1}`),
      title: String(finding.title ?? "Untitled AppSec triage finding"),
      severity: severity(finding.severity),
      confidence: confidence(finding.confidence),
      evidenceIds: array(finding.evidenceIds),
      mappings: mappings(finding),
      rationale: String(finding.rationale ?? ""),
      recommendedAction: String(finding.recommendedAction ?? "")
    })),
    recommendedNextSteps: array(value.recommendedNextSteps),
    limitations: array(value.limitations)
  };
}

function hasTriageShape(value) {
  return (
    value &&
    typeof value === "object" &&
    typeof value.summary === "string" &&
    Array.isArray(value.triageFindings) &&
    Array.isArray(value.recommendedNextSteps) &&
    Array.isArray(value.limitations)
  );
}

function triageSchema() {
  return JSON.stringify(
    {
      summary: "string",
      triageFindings: [
        {
          id: "string",
          title: "string",
          severity: "critical|high|medium|low|info",
          confidence: "high|medium|low",
          evidenceIds: ["scanner finding IDs from input.findings[].id"],
          mappings: {
            cwe: ["CWE-200"],
            owaspAsvs: ["V14.2.1"],
            owaspTop10: ["A05:2021"],
            owaspLlmTop10: []
          },
          rationale: "string",
          recommendedAction: "string"
        }
      ],
      recommendedNextSteps: ["string"],
      limitations: ["string"]
    },
    null,
    2
  );
}

function tryParseJson(value) {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

function severity(value) {
  return ["critical", "high", "medium", "low", "info"].includes(value) ? value : "info";
}

function confidence(value) {
  return ["high", "medium", "low"].includes(value) ? value : "medium";
}

function mappings(finding) {
  const existing =
    finding?.mappings && typeof finding.mappings === "object" ? finding.mappings : {};
  return {
    cwe: array(existing.cwe ?? finding?.weaknessIds),
    owaspAsvs: array(existing.owaspAsvs),
    owaspTop10: array(existing.owaspTop10),
    owaspLlmTop10: array(existing.owaspLlmTop10)
  };
}

function array(value) {
  return Array.isArray(value) ? value.map(String) : [];
}

function hash(value) {
  return createHash("sha256").update(value).digest("hex");
}
