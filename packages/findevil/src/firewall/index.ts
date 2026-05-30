import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type { TaintLedgerEntry } from "../types/taint.js";
import {
  detectImperativePattern,
  extractScriptTokens,
  matchesImperativePattern,
  type ImperativePatternMatch
} from "./patterns.js";
import { isSafelyQuoted } from "./quoting.js";

export type FirewallDecisionValue = "allow" | "block";

export interface MatchedTaintReference {
  readonly entry: TaintLedgerEntry;
  readonly matchedText: string;
  readonly matchedFullEntry: boolean;
  readonly argsPath: readonly string[];
  readonly argsText: string;
}

export interface FirewallDecision {
  readonly decision: FirewallDecisionValue;
  readonly quoted: boolean;
  readonly matchedEntries: readonly TaintLedgerEntry[];
  readonly matchedTaint: readonly MatchedTaintReference[];
  readonly matchedPatternId?: ImperativePatternMatch["id"] | undefined;
  readonly matchedPattern?: ImperativePatternMatch | undefined;
  readonly inspectedText: string;
  readonly reason: string;
}

interface StringOccurrence {
  readonly path: readonly string[];
  readonly value: string;
}

export function classifyToolCall(
  args: JsonRecord,
  taintLedger: readonly TaintLedgerEntry[]
): FirewallDecision {
  const occurrences = stringOccurrences(args);
  const inspectedText = occurrences.map((occurrence) => occurrence.value).join("\n");
  const matchedTaint = matchTaint(occurrences, taintLedger);
  const matchedPattern = findMatchedPattern(matchedTaint);
  const matchedEntries = uniqueEntries(matchedTaint);
  const quoted =
    matchedEntries.length > 0 && matchedEntries.every((entry) => isSafelyQuoted(entry, args));

  if (matchedPattern && !quoted) {
    return {
      decision: "block",
      quoted: false,
      matchedEntries,
      matchedTaint,
      matchedPatternId: matchedPattern.id,
      matchedPattern,
      inspectedText,
      reason: "Case-derived text cannot become an operational instruction."
    };
  }

  return {
    decision: "allow",
    quoted,
    matchedEntries,
    matchedTaint,
    ...(matchedPattern ? { matchedPatternId: matchedPattern.id, matchedPattern } : {}),
    inspectedText,
    reason:
      matchedEntries.length === 0
        ? "no tainted spans matched"
        : quoted
          ? "matched tainted spans are quoted as evidence"
          : "matched tainted spans did not contain imperative instructions"
  };
}

export function firewallDecisionPolicyFields(decision: FirewallDecision): JsonRecord {
  return {
    decision: decision.decision,
    quoted: String(decision.quoted),
    matchedTaint: String(decision.matchedEntries.length > 0),
    matchedPatternId: decision.matchedPatternId ?? ""
  };
}

function matchTaint(
  occurrences: readonly StringOccurrence[],
  taintLedger: readonly TaintLedgerEntry[]
): MatchedTaintReference[] {
  const matches: MatchedTaintReference[] = [];
  const seen = new Set<string>();
  for (const entry of taintLedger) {
    const needles = [entry.text, ...extractScriptTokens(entry.text)].filter((needle) =>
      Boolean(needle.trim())
    );
    for (const occurrence of occurrences) {
      for (const needle of needles) {
        if (!containsNeedle(occurrence.value, needle)) {
          continue;
        }
        const key = `${entry.id}\0${occurrence.path.join(".")}\0${needle}`;
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        matches.push({
          entry,
          matchedText: needle,
          matchedFullEntry: needle === entry.text,
          argsPath: occurrence.path,
          argsText: occurrence.value
        });
      }
    }
  }
  return matches;
}

function findMatchedPattern(
  matchedTaint: readonly MatchedTaintReference[]
): ImperativePatternMatch | undefined {
  for (const match of matchedTaint) {
    const taintedPattern = detectImperativePattern(match.entry.text);
    if (
      taintedPattern &&
      (match.matchedFullEntry || containsNeedle(match.argsText, taintedPattern.matchedText))
    ) {
      return taintedPattern;
    }
    const matchedTextPattern = detectImperativePattern(match.matchedText);
    if (matchedTextPattern) {
      return matchedTextPattern;
    }
    if (
      extractScriptTokens(match.matchedText).length > 0 &&
      matchesImperativePattern("run-named-script", match.argsText)
    ) {
      return {
        id: "run-named-script",
        rationale:
          "Named scripts from evidence can be payloads or decoys and need analysis, not execution.",
        matchedText: match.matchedText
      };
    }
  }
  return undefined;
}

function uniqueEntries(matchedTaint: readonly MatchedTaintReference[]): TaintLedgerEntry[] {
  const byId = new Map<string, TaintLedgerEntry>();
  for (const match of matchedTaint) {
    byId.set(match.entry.id, match.entry);
  }
  return [...byId.values()];
}

function stringOccurrences(input: unknown, path: readonly string[] = []): StringOccurrence[] {
  if (typeof input === "string") {
    return [{ path, value: input }];
  }
  if (Array.isArray(input)) {
    return input.flatMap((item, index) => stringOccurrences(item, [...path, String(index)]));
  }
  if (input && typeof input === "object") {
    return Object.entries(input).flatMap(([key, value]) =>
      stringOccurrences(value, [...path, key])
    );
  }
  return [];
}

function containsNeedle(value: string, needle: string): boolean {
  return value.includes(needle) || normalizeWhitespace(value).includes(normalizeWhitespace(needle));
}

function normalizeWhitespace(value: string): string {
  return value.replace(/\s+/gu, " ").trim();
}

export * from "./patterns.js";
export * from "./quoting.js";
export * from "./repair.js";
export * from "./writer.js";
