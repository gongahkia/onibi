import { builtinSkills } from "./builtins.js";
import type { SkillLookupQuery, SkillMatch, SkillMetadata, SkillSelection } from "./types.js";

export const skillReuseThreshold = 70;

export class SkillNotFoundError extends Error {
  public constructor(query: SkillLookupQuery) {
    super(`No deterministic skill matched query ${JSON.stringify(query)}.`);
    this.name = "SkillNotFoundError";
  }
}

export function listSkills(): readonly SkillMetadata[] {
  return builtinSkills;
}

export function getSkill(skillId: string): SkillMetadata | undefined {
  return builtinSkills.find((skill) => skill.id === skillId);
}

export function requireSkill(skillId: string): SkillMetadata {
  const skill = getSkill(skillId);
  if (!skill) {
    throw new SkillNotFoundError({ skillId });
  }

  return skill;
}

export function lookupSkills(query: SkillLookupQuery): readonly SkillMetadata[] {
  return matchSkills(query).map((match) => match.skill);
}

export function matchSkills(query: SkillLookupQuery): readonly SkillMatch[] {
  return builtinSkills
    .map((skill) => scoreSkill(skill, query))
    .filter((match) => match.score > 0)
    .sort((left, right) => right.score - left.score || left.skill.id.localeCompare(right.skill.id));
}

export function chooseSkillOrCodegen(query: SkillLookupQuery): SkillSelection {
  const [bestMatch] = matchSkills(query);
  if (bestMatch && bestMatch.score >= skillReuseThreshold) {
    return {
      kind: "skill",
      match: bestMatch
    };
  }

  return {
    kind: "codegen",
    score: bestMatch?.score ?? 0,
    reasons: [
      bestMatch
        ? `Best registry match '${bestMatch.skill.id}' scored ${bestMatch.score}, below reuse threshold ${skillReuseThreshold}.`
        : "No registry skill matched the requested operation.",
      "Create a codegen node with explicit provenance and replay metadata."
    ]
  };
}

export function findDefaultSkill(query: Omit<SkillLookupQuery, "skillId">): SkillMetadata {
  const selection = chooseSkillOrCodegen(query);
  if (selection.kind === "codegen") {
    throw new SkillNotFoundError(query);
  }

  return selection.match.skill;
}

function scoreSkill(skill: SkillMetadata, query: SkillLookupQuery): SkillMatch {
  if (query.skillId && query.skillId !== skill.id) {
    return {
      skill,
      score: 0,
      reasons: []
    };
  }

  const reasons: string[] = [];
  let score = 0;

  if (query.skillId === skill.id) {
    score += 100;
    reasons.push(`Exact skill id '${skill.id}' matched.`);
  }

  if (query.capability && skill.capabilities.includes(query.capability)) {
    score += 40;
    reasons.push(`Capability '${query.capability}' matched.`);
  }

  if (query.nodeKind && skill.nodeKinds.includes(query.nodeKind)) {
    score += 25;
    reasons.push(`Node kind '${query.nodeKind}' matched.`);
  }

  const adapterScore = scoreAdapterDependencies(skill, query.adapterDependencies ?? []);
  if (adapterScore > 0) {
    score += adapterScore;
    reasons.push("Adapter dependencies matched.");
  }

  const promptScore = scorePrompt(skill, query.prompt);
  if (promptScore > 0) {
    score += promptScore;
    reasons.push("Prompt matched registry guidance.");
  }

  return {
    skill,
    score: Math.min(score, 100),
    reasons
  };
}

function scoreAdapterDependencies(skill: SkillMetadata, adapters: readonly string[]): number {
  if (adapters.length === 0) {
    return 0;
  }

  return adapters.every((adapter) => skill.adapterDependencies.includes(adapter)) ? 25 : 0;
}

function scorePrompt(skill: SkillMetadata, prompt: string | undefined): number {
  if (!prompt) {
    return 0;
  }

  const haystack = [
    skill.name,
    skill.description,
    skill.metaprompt,
    ...skill.capabilities,
    ...skill.adapterDependencies
  ]
    .join(" ")
    .toLowerCase();
  const matches = tokenize(prompt).filter((token) => haystack.includes(token));

  return Math.min(matches.length * 5, 10);
}

function tokenize(value: string): readonly string[] {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/u)
    .filter((token) => token.length > 2);
}
