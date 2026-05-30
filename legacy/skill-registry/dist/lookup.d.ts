import type { SkillLookupQuery, SkillMatch, SkillMetadata, SkillSelection } from "./types.js";
export declare const skillReuseThreshold = 70;
export declare class SkillNotFoundError extends Error {
    constructor(query: SkillLookupQuery);
}
export declare function listSkills(): readonly SkillMetadata[];
export declare function getSkill(skillId: string): SkillMetadata | undefined;
export declare function requireSkill(skillId: string): SkillMetadata;
export declare function lookupSkills(query: SkillLookupQuery): readonly SkillMetadata[];
export declare function matchSkills(query: SkillLookupQuery): readonly SkillMatch[];
export declare function registerPromotedSkill(skill: SkillMetadata): SkillMetadata;
export declare function loadPromotedSkills(skills: readonly SkillMetadata[]): readonly SkillMetadata[];
export declare function clearPromotedSkillsForTests(): void;
export declare function chooseSkillOrCodegen(query: SkillLookupQuery): SkillSelection;
export declare function findDefaultSkill(query: Omit<SkillLookupQuery, "skillId">): SkillMetadata;
//# sourceMappingURL=lookup.d.ts.map