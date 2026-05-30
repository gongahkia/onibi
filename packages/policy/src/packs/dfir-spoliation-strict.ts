import type { PolicyPackMetadata } from "../packs.js";
import { resolve } from "node:path";
import type { PolicyRule, PolicyRuleSet } from "../types.js";

export interface DfirSpoliationStrictConfig {
  readonly evidenceRoot: string;
  readonly derivedWorkspace: string;
}

interface DfirPolicyRule extends PolicyRule {
  readonly reason?: string | undefined;
}

interface DfirPolicyRuleSet extends PolicyRuleSet {
  readonly rules: readonly DfirPolicyRule[];
}

interface PolicyPack {
  readonly id: "dfir-spoliation-strict";
  readonly description: string;
  readonly metadata: PolicyPackMetadata;
  readonly config: DfirSpoliationStrictConfig;
  readonly ruleset: DfirPolicyRuleSet;
}

const writeTools = ["Write", "Edit", "MultiEdit"] as const;
const pathArgFields = [
  "path",
  "filePath",
  "file_path",
  "targetPath",
  "target",
  "outputPath",
  "destination",
  "dest",
  "pattern",
  "glob"
] as const;

export function createDfirSpoliationStrictPolicyPack(
  config: DfirSpoliationStrictConfig
): PolicyPack {
  const evidencePrefix = pathPrefixRegexSource(config.evidenceRoot);
  const derivedPrefix = pathPrefixRegexSource(config.derivedWorkspace);
  const evidencePathRegex = `^${evidencePrefix}`;
  const outsideDerivedWorkspaceRegex = `^(?!${evidencePrefix})(?!${derivedPrefix}).+`;
  const destructiveShellRegex = `\\b(?:rm|del|mv|dd)\\b[^\\n]*${pathReferenceRegexSource(
    config.evidenceRoot
  )}`;

  return {
    id: "dfir-spoliation-strict",
    description: "Strict DFIR evidence spoliation guardrails for Protocol SIFT runs.",
    config: normalizeConfig(config),
    metadata: {
      version: "0.2.0",
      region: "global",
      maturity: "experimental",
      controlMappings: ["dfir:evidence-integrity", "dfir:spoliation-prevention"],
      changelog: [
        "Replaced Phase 1 stub with configurable evidence-root write denial and derived workspace review rules."
      ]
    },
    ruleset: {
      rules: [
        {
          id: "deny-write-into-evidence-root",
          when: pathFieldExpression(evidencePathRegex),
          action: "deny",
          reason: "Original evidence is read-only per dfir-spoliation policy."
        },
        {
          id: "deny-destructive-shell-in-evidence-root",
          when: `tool == "Bash" && args.command =~ "${destructiveShellRegex}"`,
          action: "deny",
          reason: "Destructive shell commands must not target original evidence."
        },
        {
          id: "warn-write-outside-derived-workspace",
          when: pathFieldExpression(outsideDerivedWorkspaceRegex),
          action: "require-approval",
          approverRole: "dfir-reviewer",
          reason: "Derived artifacts should stay inside the configured derived workspace."
        }
      ]
    }
  };
}

export const dfirSpoliationStrictPolicyPack = createDfirSpoliationStrictPolicyPack({
  evidenceRoot: "/evidence",
  derivedWorkspace: "/derived"
});

function pathFieldExpression(regex: string): string {
  return writeTools
    .flatMap((tool) =>
      pathArgFields.map((field) => `tool == "${tool}" && args.${field} =~ "${regex}"`)
    )
    .join(" || ");
}

function normalizeConfig(config: DfirSpoliationStrictConfig): DfirSpoliationStrictConfig {
  return {
    evidenceRoot: normalizePolicyPath(config.evidenceRoot),
    derivedWorkspace: normalizePolicyPath(config.derivedWorkspace)
  };
}

function pathPrefixRegexSource(path: string): string {
  const alternatives = pathVariants(path).map((variant) =>
    variant === "/" ? "/" : `${escapeRegex(variant)}(?:$|/)`
  );
  return `(?:${alternatives.join("|")})`;
}

function pathReferenceRegexSource(path: string): string {
  const alternatives = pathVariants(path).map((variant) =>
    variant === "/" ? "/" : `${escapeRegex(variant)}(?:/|$|(?![A-Za-z0-9._-]))`
  );
  return `(?:${alternatives.join("|")})`;
}

function pathVariants(path: string): readonly string[] {
  return [...new Set([normalizePolicyPath(path), normalizePolicyPath(resolve(path))])];
}

function normalizePolicyPath(path: string): string {
  const trimmed = path.trim();
  if (trimmed.length === 0) {
    throw new Error("DFIR spoliation policy paths cannot be empty.");
  }
  if (/["\n\r]/u.test(trimmed)) {
    throw new Error("DFIR spoliation policy paths cannot contain quotes or newlines.");
  }

  const normalized = trimmed.replace(/\\/gu, "/").replace(/\/+/gu, "/");
  return normalized === "/" ? normalized : normalized.replace(/\/+$/u, "");
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}
