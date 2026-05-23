import type { JsonValue } from "@kelpclaw/workflow-spec";
import { PolicyExpressionError } from "./types.js";
import type { PolicyContext } from "./types.js";

type JsonPathRoot = "args" | "skill";

export function evaluatePolicyExpression(expression: string, context: PolicyContext): boolean {
  const trimmed = expression.trim();
  if (!trimmed) {
    throw new PolicyExpressionError("Policy expression cannot be empty.");
  }

  const orParts = splitOperator(trimmed, "||");
  if (orParts.length > 1) {
    return orParts.some((part) => evaluatePolicyExpression(part, context));
  }

  const andParts = splitOperator(trimmed, "&&");
  if (andParts.length > 1) {
    return andParts.every((part) => evaluatePolicyExpression(part, context));
  }

  return evaluateAtom(trimmed, context);
}

export function validatePolicyExpression(expression: string): void {
  evaluatePolicyExpression(expression, {
    tool: "",
    args: {}
  });
}

function evaluateAtom(atom: string, context: PolicyContext): boolean {
  const toolEquals = /^tool\s*==\s*"([^"]+)"$/u.exec(atom);
  if (toolEquals?.[1]) {
    return context.tool === toolEquals[1];
  }

  const toolStartsWith = /^tool\s+startsWith\s+"([^"]+)"$/u.exec(atom);
  if (toolStartsWith?.[1]) {
    return context.tool.startsWith(toolStartsWith[1]);
  }

  const classificationEquals = /^classification\s*==\s*"([^"]+)"$/u.exec(atom);
  if (classificationEquals?.[1]) {
    return context.classification === classificationEquals[1];
  }

  const hasClassification = /^hasClassification\s+"([^"]+)"$/u.exec(atom);
  if (hasClassification?.[1]) {
    return context.classification === hasClassification[1];
  }

  const includes = /^(skill(?:\.[a-zA-Z0-9_-]+)+)\s+includes\s+"([^"]+)"$/u.exec(atom);
  if (includes?.[1] && includes[2]) {
    const value = readPath(includes[1], context);
    return Array.isArray(value) && value.some((entry) => entry === includes[2]);
  }

  const regex = /^(args(?:\.[a-zA-Z0-9_-]+)+)\s*=~\s*"([^"]+)"$/u.exec(atom);
  if (regex?.[1] && regex[2]) {
    const pattern = createPolicyRegex(regex[2]);
    const value = readPath(regex[1], context);
    return typeof value === "string" && pattern.test(value);
  }

  const pathEquals = /^((?:args|skill)(?:\.[a-zA-Z0-9_-]+)+)\s*==\s*"([^"]*)"$/u.exec(atom);
  if (pathEquals?.[1] && pathEquals[2] !== undefined) {
    return readPath(pathEquals[1], context) === pathEquals[2];
  }

  throw new PolicyExpressionError(`Unsupported policy expression: ${atom}`);
}

function createPolicyRegex(pattern: string): RegExp {
  try {
    return new RegExp(pattern, "u");
  } catch (error) {
    throw new PolicyExpressionError(
      `Invalid policy regex '${pattern}': ${error instanceof Error ? error.message : String(error)}`
    );
  }
}

function splitOperator(expression: string, operator: "&&" | "||"): readonly string[] {
  const parts: string[] = [];
  let quoteOpen = false;
  let start = 0;
  for (let index = 0; index < expression.length; index += 1) {
    const char = expression[index];
    if (char === '"' && expression[index - 1] !== "\\") {
      quoteOpen = !quoteOpen;
    }
    if (!quoteOpen && expression.slice(index, index + operator.length) === operator) {
      parts.push(expression.slice(start, index).trim());
      start = index + operator.length;
      index += operator.length - 1;
    }
  }
  if (parts.length === 0) {
    return [expression];
  }
  parts.push(expression.slice(start).trim());
  return parts;
}

function readPath(path: string, context: PolicyContext): JsonValue | readonly string[] | undefined {
  const [root, ...segments] = path.split(".") as [JsonPathRoot, ...string[]];
  let value: unknown =
    root === "args"
      ? context.args
      : (context.skill as Record<string, JsonValue | readonly string[] | undefined> | undefined);
  for (const segment of segments) {
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
      return undefined;
    }
    value = (value as Record<string, JsonValue | readonly string[] | undefined>)[segment];
  }
  return isJsonValueOrStringArray(value) ? value : undefined;
}

function isJsonValueOrStringArray(value: unknown): value is JsonValue | readonly string[] {
  if (
    value === null ||
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return true;
  }
  if (Array.isArray(value)) {
    return value.every((entry) => isJsonValueOrStringArray(entry));
  }
  if (typeof value === "object") {
    return Object.values(value).every((entry) => isJsonValueOrStringArray(entry));
  }
  return false;
}
