import { describe, expect, it } from "vitest";
import {
  chooseSkillOrCodegen,
  findDefaultSkill,
  getSkill,
  listSkills,
  lookupSkills,
  matchSkills,
  requireSkill,
  skillReuseThreshold
} from "../src/index.js";

describe("skill registry", () => {
  it("ships deterministic builtin skills with Phase 2 metadata", () => {
    expect(listSkills().every((skill) => skill.deterministic)).toBe(true);
    expect(listSkills().every((skill) => skill.runtimeTemplate.image.length > 0)).toBe(true);
    expect(listSkills().every((skill) => skill.validationRules.length > 0)).toBe(true);
    expect(listSkills().every((skill) => skill.examples.length > 0)).toBe(true);
  });

  it("looks up skills by exact id", () => {
    expect(getSkill("skill.gmail.receipts.read")?.name).toBe("Read Gmail Receipts");
  });

  it("finds a default skill by node kind and capability", () => {
    expect(
      findDefaultSkill({
        nodeKind: "delivery",
        capability: "sheets-rows-append",
        adapterDependencies: ["adapter.sheets.fake"]
      }).id
    ).toBe("skill.sheets.rows.append");
  });

  it("filters skills by capability through deterministic matching", () => {
    expect(lookupSkills({ capability: "gmail-receipts-read" }).map((skill) => skill.id)).toEqual([
      "skill.gmail.receipts.read"
    ]);
  });

  it("returns explainable skill match scores", () => {
    const [match] = matchSkills({
      nodeKind: "skill",
      capability: "gmail-receipts-read",
      adapterDependencies: ["adapter.gmail.fake"],
      prompt: "Read Gmail receipt emails"
    });

    expect(match?.skill.id).toBe("skill.gmail.receipts.read");
    expect(match?.score).toBeGreaterThanOrEqual(skillReuseThreshold);
    expect(match?.reasons).toContain("Capability 'gmail-receipts-read' matched.");
  });

  it("prefers registry skill reuse over codegen when the threshold is met", () => {
    const selection = chooseSkillOrCodegen({
      nodeKind: "delivery",
      capability: "sheets-rows-append",
      adapterDependencies: ["adapter.sheets.fake"],
      prompt: "append rows to a Google Sheet"
    });

    expect(selection.kind).toBe("skill");
    if (selection.kind === "skill") {
      expect(selection.match.skill.id).toBe("skill.sheets.rows.append");
      expect(selection.match.score).toBeGreaterThanOrEqual(skillReuseThreshold);
    }
  });

  it("falls back to codegen with reasons when no skill covers the operation", () => {
    const selection = chooseSkillOrCodegen({
      nodeKind: "codegen",
      prompt: "scrape a custom public status page"
    });

    expect(selection.kind).toBe("codegen");
    if (selection.kind === "codegen") {
      expect(selection.reasons.join(" ")).toContain("codegen node");
    }
  });

  it("throws for unknown required skills", () => {
    expect(() => requireSkill("skill.missing")).toThrow("No deterministic skill matched");
  });
});
