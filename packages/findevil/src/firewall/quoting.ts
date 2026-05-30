import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type { TaintLedgerEntry } from "../types/taint.js";

type QuotedSpan = string | Pick<TaintLedgerEntry, "text">;

const safeKeyPattern = /\b(?:evidence|log|note|ransom|artifact)\b/iu;
const safeValueContextPattern = /\b(?:evidence|log|note|ransom|artifact)\b/iu;

export function isSafelyQuoted(span: QuotedSpan, args: JsonRecord): boolean {
  const text = typeof span === "string" ? span : span.text;
  if (!text.trim()) {
    return false;
  }

  const occurrences = findStringOccurrences(args, text);
  return (
    occurrences.length > 0 &&
    occurrences.every((occurrence) =>
      isSafeStringOccurrence(occurrence.value, text, occurrence.pathSegments)
    )
  );
}

function isSafeStringOccurrence(
  value: string,
  text: string,
  pathSegments: readonly string[]
): boolean {
  if (pathSegments.some((segment) => safeKeyPattern.test(segment))) {
    return true;
  }
  if (containsMarkdownQuote(value, text)) {
    return true;
  }
  return safeValueContextPattern.test(value) && containsQuotedText(value, text);
}

function findStringOccurrences(
  input: unknown,
  needle: string,
  pathSegments: readonly string[] = []
): { readonly value: string; readonly pathSegments: readonly string[] }[] {
  if (typeof input === "string") {
    return containsNeedle(input, needle) ? [{ value: input, pathSegments }] : [];
  }
  if (Array.isArray(input)) {
    return input.flatMap((item, index) =>
      findStringOccurrences(item, needle, [...pathSegments, String(index)])
    );
  }
  if (input && typeof input === "object") {
    return Object.entries(input).flatMap(([key, value]) =>
      findStringOccurrences(value, needle, [...pathSegments, key])
    );
  }
  return [];
}

function containsNeedle(value: string, needle: string): boolean {
  return value.includes(needle) || normalizeWhitespace(value).includes(normalizeWhitespace(needle));
}

function containsQuotedText(value: string, needle: string): boolean {
  const escaped = escapeRegex(needle);
  const normalizedEscaped = escapeRegex(normalizeWhitespace(needle));
  return (
    new RegExp(`["'\`]\\s*${escaped}\\s*["'\`]`, "u").test(value) ||
    new RegExp(`["'\`]\\s*${normalizedEscaped}\\s*["'\`]`, "u").test(normalizeWhitespace(value))
  );
}

function containsMarkdownQuote(value: string, needle: string): boolean {
  const blocks: string[] = [];
  let current: string[] = [];
  for (const line of value.split(/\r?\n/u)) {
    if (/^\s*>\s?/u.test(line)) {
      current.push(line.replace(/^\s*>\s?/u, ""));
      continue;
    }
    if (current.length > 0) {
      blocks.push(current.join("\n"));
      current = [];
    }
  }
  if (current.length > 0) {
    blocks.push(current.join("\n"));
  }
  return blocks.some((block) => containsNeedle(block, needle));
}

function normalizeWhitespace(value: string): string {
  return value.replace(/\s+/gu, " ").trim();
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}
