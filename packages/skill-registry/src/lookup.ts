import { builtinSkills } from "./builtins.js";
import type { SkillLookupQuery, SkillMetadata } from "./types.js";

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
  return builtinSkills.filter((skill) => {
    if (query.skillId && skill.id !== query.skillId) {
      return false;
    }

    if (query.nodeType && !skill.nodeTypes.includes(query.nodeType)) {
      return false;
    }

    if (query.capability && !skill.capabilities.includes(query.capability)) {
      return false;
    }

    return true;
  });
}

export function findDefaultSkill(query: Omit<SkillLookupQuery, "skillId">): SkillMetadata {
  const [skill] = lookupSkills(query);
  if (!skill) {
    throw new SkillNotFoundError(query);
  }

  return skill;
}
