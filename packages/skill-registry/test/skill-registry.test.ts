import { describe, expect, it } from "vitest";
import {
  findDefaultSkill,
  getSkill,
  listSkills,
  lookupSkills,
  requireSkill
} from "../src/index.js";

describe("skill registry", () => {
  it("ships deterministic builtin skills", () => {
    expect(listSkills()).toHaveLength(5);
    expect(listSkills().every((skill) => skill.deterministic)).toBe(true);
  });

  it("looks up skills by exact id", () => {
    expect(getSkill("skill.codegen.typescript")?.name).toBe("TypeScript Codegen");
  });

  it("finds a default skill by node type and capability", () => {
    expect(findDefaultSkill({ nodeType: "adapter", capability: "adapter-dispatch" }).id).toBe(
      "skill.adapter.dispatch"
    );
  });

  it("filters skills by capability", () => {
    expect(lookupSkills({ capability: "brief-ingestion" }).map((skill) => skill.id)).toEqual([
      "skill.read-brief"
    ]);
  });

  it("throws for unknown required skills", () => {
    expect(() => requireSkill("skill.missing")).toThrow("No deterministic skill matched");
  });
});
