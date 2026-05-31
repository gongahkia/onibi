import { existsSync, readFileSync } from "node:fs";
import type { Claim, EvidenceRef } from "../types/claim.js";
import { hashEvidenceRow } from "./hashing.js";

export interface YaraMatch {
  readonly artifact: string;
  readonly rule: string;
  readonly target: string;
  readonly namespace?: string | undefined;
  readonly tags: readonly string[];
  readonly meta: Readonly<Record<string, string | number | boolean>>;
  readonly strings: readonly YaraStringMatch[];
  readonly raw: unknown;
}

export interface YaraStringMatch {
  readonly identifier?: string | undefined;
  readonly offset?: number | undefined;
  readonly length?: number | undefined;
  readonly data?: string | undefined;
  readonly raw: unknown;
}

type JsonObject = Record<string, unknown>;

const genericRuleTerms = new Set([
  "apt",
  "family",
  "generic",
  "loader",
  "mal",
  "malware",
  "packed",
  "packer",
  "rule",
  "trojan",
  "win",
  "windows",
  "yara"
]);

export function parseYaraJson(file: string, artifact?: string): YaraMatch[] {
  const input = yaraInput(file, artifact);
  return parseJsonRecords(input.contents).flatMap((record) =>
    collectYaraMatches(record, input.artifact)
  );
}

export function matchYaraFamilyHit(
  claim: Pick<Claim, "text" | "type">,
  matches: readonly YaraMatch[]
): EvidenceRef[] {
  if (claim.type !== "malware_identification") {
    return [];
  }
  const claimText = normalizedSearchText(claim.text);
  return matches
    .filter((match) => yaraFamilyTerms(match).some((term) => includesSearchTerm(claimText, term)))
    .map(yaraMatchToEvidenceRef);
}

export function matchYaraExecutionContext(
  claim: Pick<Claim, "text" | "type">,
  matches: readonly YaraMatch[]
): EvidenceRef[] {
  if (claim.type !== "program_execution") {
    return [];
  }
  const terms = fileTerms(claim.text);
  if (terms.length === 0) {
    return [];
  }
  return matches
    .filter((match) => terms.some((term) => targetMatchesTerm(match.target, term)))
    .map(yaraMatchToEvidenceRef);
}

function yaraInput(
  file: string,
  artifact: string | undefined
): { contents: string; artifact: string } {
  const trimmed = file.trimStart();
  if (
    !trimmed.startsWith("{") &&
    !trimmed.startsWith("[") &&
    !file.includes("\n") &&
    existsSync(file)
  ) {
    return { contents: readFileSync(file, "utf8"), artifact: artifact ?? file };
  }
  return { contents: file, artifact: artifact ?? "yara.json" };
}

function parseJsonRecords(input: string): unknown[] {
  const trimmed = input.trim();
  if (trimmed.length === 0) {
    return [];
  }
  try {
    return recordsFromParsedJson(JSON.parse(trimmed) as unknown);
  } catch {
    return trimmed
      .split(/\r?\n/u)
      .filter((line) => line.trim().length > 0)
      .flatMap((line) => recordsFromParsedJson(JSON.parse(line) as unknown));
  }
}

function recordsFromParsedJson(parsed: unknown): unknown[] {
  if (Array.isArray(parsed)) {
    return parsed;
  }
  const object = asObject(parsed);
  if (!object) {
    return [];
  }
  for (const key of ["scan", "Scan", "results", "Results", "records", "Records"]) {
    const value = object[key];
    if (Array.isArray(value)) {
      return value;
    }
  }
  return [parsed];
}

function collectYaraMatches(
  record: unknown,
  artifact: string,
  inheritedTarget?: string | undefined
): YaraMatch[] {
  if (Array.isArray(record)) {
    return record.flatMap((item) => collectYaraMatches(item, artifact, inheritedTarget));
  }
  const object = asObject(record);
  if (!object) {
    return [];
  }

  const target =
    stringField(object, [
      "target",
      "path",
      "file",
      "filename",
      "filePath",
      "filepath",
      "matched_file"
    ]) ?? inheritedTarget;
  const matchesValue = field(object, "matches");
  const isExplicitNoMatch =
    matchesValue === false ||
    field(object, "matched") === false ||
    field(object, "is_match") === false;
  const rule = ruleName(object);
  if (rule && target && !isExplicitNoMatch) {
    return [
      {
        artifact,
        rule,
        target,
        ...(stringField(object, ["namespace", "ruleNamespace", "rule_namespace"]) !== undefined
          ? { namespace: stringField(object, ["namespace", "ruleNamespace", "rule_namespace"]) }
          : {}),
        tags: tagsValue(firstDefined([field(object, "tags"), field(object, "Tags")])),
        meta: metaValue(
          firstDefined([field(object, "meta"), field(object, "metadata"), field(object, "metas")])
        ),
        strings: stringsValue(firstDefined([field(object, "strings"), field(object, "patterns")])),
        raw: record
      }
    ];
  }

  const nestedMatches = arrayField(object, "matches");
  if (nestedMatches) {
    return nestedMatches.flatMap((item) => collectYaraMatches(item, artifact, target));
  }
  const nestedRules = firstArrayField(object, ["rules", "Rules", "matchedRules", "matched_rules"]);
  if (nestedRules) {
    return nestedRules.flatMap((item) => collectYaraMatches(item, artifact, target));
  }
  return [];
}

function ruleName(object: JsonObject): string | undefined {
  return stringField(object, ["rule", "ruleName", "rule_name", "identifier", "name"]);
}

function tagsValue(input: unknown): string[] {
  if (Array.isArray(input)) {
    return input.flatMap((item) => {
      const value = stringValue(item);
      return value ? [value] : [];
    });
  }
  const value = stringValue(input);
  return value ? value.split(/[, ]+/u).filter((item) => item.length > 0) : [];
}

function metaValue(input: unknown): Record<string, string | number | boolean> {
  if (Array.isArray(input)) {
    return Object.assign({}, ...input.map((item) => metaValue(item))) as Record<
      string,
      string | number | boolean
    >;
  }
  const object = asObject(input);
  if (!object) {
    return {};
  }
  const namedKey = stringField(object, ["identifier", "name", "key"]);
  const namedValue = scalarValue(firstDefined([field(object, "value"), field(object, "Value")]));
  if (namedKey && namedValue !== undefined) {
    return { [namedKey]: namedValue };
  }

  const meta: Record<string, string | number | boolean> = {};
  for (const [key, value] of Object.entries(object)) {
    const scalar = scalarValue(value);
    if (scalar !== undefined) {
      meta[key] = scalar;
    }
  }
  return meta;
}

function stringsValue(input: unknown): YaraStringMatch[] {
  const strings = Array.isArray(input) ? input : [];
  return strings.flatMap((item) => {
    const object = asObject(item);
    if (!object) {
      return [];
    }
    const identifier = stringField(object, ["identifier", "name", "string", "pattern"]);
    const instances = firstArrayField(object, ["matches", "instances"]);
    if (instances) {
      return instances.map((instance) => stringMatchValue(instance, identifier));
    }
    return [stringMatchValue(item, identifier)];
  });
}

function stringMatchValue(input: unknown, identifier: string | undefined): YaraStringMatch {
  const object = asObject(input);
  const offset = object ? numberField(object, ["offset", "Offset"]) : undefined;
  const length = object
    ? numberField(object, ["length", "Length", "matched_length", "matchedLength"])
    : undefined;
  const data = object
    ? stringField(object, ["data", "matched_data", "matchedData", "value", "Value"])
    : undefined;
  return {
    ...(identifier ? { identifier } : {}),
    ...(offset !== undefined ? { offset } : {}),
    ...(length !== undefined ? { length } : {}),
    ...(data ? { data } : {}),
    raw: input
  };
}

function yaraFamilyTerms(match: YaraMatch): string[] {
  const terms = new Set<string>();
  addRuleTerms(terms, match.rule);
  for (const [key, value] of Object.entries(match.meta)) {
    const normalizedKey = normalizeToken(key);
    if (
      normalizedKey.includes("family") ||
      normalizedKey.includes("malware") ||
      normalizedKey.includes("threat") ||
      normalizedKey === "name"
    ) {
      addRuleTerms(terms, String(value));
    }
  }
  for (const tag of match.tags) {
    addRuleTerms(terms, tag);
  }
  return [...terms];
}

function addRuleTerms(terms: Set<string>, value: string): void {
  const normalized = normalizedSearchText(value);
  if (
    normalized.length >= 4 &&
    (normalized.includes(" ") || !genericRuleTerms.has(normalizeToken(normalized)))
  ) {
    terms.add(normalized);
  }
  for (const token of normalized.matchAll(/\b[a-z0-9][a-z0-9._-]{3,}\b/gu)) {
    const term = normalizeToken(token[0]);
    if (term.length >= 4 && !genericRuleTerms.has(term)) {
      terms.add(term);
    }
  }
}

function fileTerms(text: string): string[] {
  const terms = new Set<string>();
  const normalized = normalizePathText(text);
  for (const match of normalized.matchAll(/\b[a-z]:\/[^\s"'`]+/giu)) {
    terms.add(stripTrailingPunctuation(match[0]));
  }
  for (const match of normalized.matchAll(/(?:^|[\s"'`(])\/[a-z0-9._~/-]+/giu)) {
    const value = stripTrailingPunctuation(match[0].trim());
    if (value.includes(".")) {
      terms.add(value);
    }
  }
  for (const match of normalized.matchAll(
    /\b[a-z0-9_.-]+\.(?:exe|dll|ps1|bat|cmd|vbs|js|msi|scr|sys|com)\b/giu
  )) {
    terms.add(match[0]);
  }
  return [...terms].filter((term) => term.length >= 4);
}

function targetMatchesTerm(target: string, term: string): boolean {
  const normalizedTarget = normalizePathText(target);
  const normalizedTerm = normalizePathText(term);
  if (normalizedTerm.includes("/")) {
    return normalizedTarget.includes(normalizedTerm);
  }
  return pathBasename(normalizedTarget) === normalizedTerm;
}

function yaraMatchToEvidenceRef(match: YaraMatch): EvidenceRef {
  return {
    artifact: match.artifact,
    locator: `yara:rule=${match.rule}:target=${match.target}`,
    supports: "yara_hit",
    hash: hashEvidenceRow({
      rule: match.rule,
      target: match.target,
      namespace: match.namespace,
      tags: match.tags,
      meta: match.meta,
      strings: match.strings
    })
  };
}

function includesSearchTerm(text: string, term: string): boolean {
  const normalized = normalizedSearchText(term);
  if (normalized.length < 4) {
    return false;
  }
  if (normalized.includes(" ")) {
    return text.includes(normalized);
  }
  return new RegExp(`(^|[^a-z0-9_.-])${escapeRegExp(normalized)}($|[^a-z0-9_.-])`, "u").test(text);
}

function normalizedSearchText(value: string): string {
  return value.toLowerCase().replace(/[_-]+/gu, " ").replace(/\s+/gu, " ").trim();
}

function normalizePathText(value: string): string {
  return value.toLowerCase().replace(/\\/gu, "/");
}

function normalizeToken(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "");
}

function pathBasename(value: string): string {
  return value.split("/").filter(Boolean).at(-1) ?? value;
}

function stripTrailingPunctuation(value: string): string {
  return value.replace(/[),.;:]+$/u, "");
}

function firstArrayField(object: JsonObject, names: readonly string[]): unknown[] | undefined {
  for (const name of names) {
    const value = arrayField(object, name);
    if (value) {
      return value;
    }
  }
  return undefined;
}

function arrayField(object: JsonObject, name: string): unknown[] | undefined {
  const value = field(object, name);
  return Array.isArray(value) ? value : undefined;
}

function stringField(object: JsonObject, names: readonly string[]): string | undefined {
  for (const name of names) {
    const value = stringValue(field(object, name));
    if (value) {
      return value;
    }
  }
  return undefined;
}

function numberField(object: JsonObject, names: readonly string[]): number | undefined {
  for (const name of names) {
    const value = field(object, name);
    const parsed =
      typeof value === "number" ? value : typeof value === "string" ? Number(value) : undefined;
    if (parsed !== undefined && Number.isFinite(parsed)) {
      return parsed;
    }
  }
  return undefined;
}

function field(object: JsonObject, name: string): unknown {
  if (name in object) {
    return object[name];
  }
  const normalized = normalizeToken(name);
  return Object.entries(object).find(([key]) => normalizeToken(key) === normalized)?.[1];
}

function firstDefined(values: readonly unknown[]): unknown {
  return values.find((value) => value !== undefined && value !== null && stringValue(value) !== "");
}

function scalarValue(input: unknown): string | number | boolean | undefined {
  if (typeof input === "string") {
    return input.trim() || undefined;
  }
  if (typeof input === "number" && Number.isFinite(input)) {
    return input;
  }
  if (typeof input === "boolean") {
    return input;
  }
  return stringValue(input);
}

function stringValue(input: unknown): string | undefined {
  if (typeof input === "string") {
    return input.trim();
  }
  if (typeof input === "number" || typeof input === "boolean") {
    return String(input);
  }
  const object = asObject(input);
  if (!object) {
    return undefined;
  }
  return stringValue(firstDefined([object["#text"], object._, object.Value, object.value]));
}

function asObject(input: unknown): JsonObject | undefined {
  return input !== null && typeof input === "object" && !Array.isArray(input)
    ? (input as JsonObject)
    : undefined;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}
