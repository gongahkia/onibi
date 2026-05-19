import { afterEach, describe, expect, it } from "vitest";
import {
  chooseSkillOrCodegen,
  clearPromotedSkillsForTests,
  findDefaultSkill,
  getSkill,
  listSkills,
  lookupSkills,
  matchSkills,
  registerPromotedSkill,
  requireSkill,
  skillReuseThreshold
} from "../src/index.js";
import type { SkillMetadata } from "../src/index.js";

afterEach(() => {
  clearPromotedSkillsForTests();
});

describe("skill registry", () => {
  it("ships deterministic builtin skills with Phase 2 metadata", () => {
    expect(listSkills().every((skill) => skill.deterministic)).toBe(true);
    expect(listSkills().every((skill) => skill.runtimeTemplate.image.length > 0)).toBe(true);
    expect(listSkills().every((skill) => skill.validationRules.length > 0)).toBe(true);
    expect(listSkills().every((skill) => skill.examples.length > 0)).toBe(true);
    expect(listSkills().every((skill) => Array.isArray(skill.adapterOperations))).toBe(true);
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

  it("declares operation-level adapter dependencies for integration skills", () => {
    expect(requireSkill("skill.gmail.receipts.read").adapterOperations).toEqual([
      {
        adapterId: "adapter.gmail.fake",
        operation: "gmail.receipts.search",
        operationVersion: "1.0.0"
      }
    ]);
    expect(requireSkill("skill.email.results.deliver").requiredSecrets).toEqual([
      "email.delivery"
    ]);
    expect(requireSkill("skill.alert.push.dispatch").adapterDependencies).toEqual([
      "adapter.whatsapp.fake",
      "adapter.telegram.fake"
    ]);
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

  it("registers promoted skills so future matching reuses them instead of codegen", () => {
    expect(
      chooseSkillOrCodegen({
        nodeKind: "skill",
        capability: "public-status-scrape",
        prompt: "scrape a custom public status page"
      }).kind
    ).toBe("codegen");

    const promoted = registerPromotedSkill(promotedScraperSkill());
    const selection = chooseSkillOrCodegen({
      nodeKind: "skill",
      capability: "public-status-scrape",
      prompt: "scrape a custom public status page"
    });

    expect(promoted.source).toBe("promoted");
    expect(getSkill(promoted.id)?.source).toBe("promoted");
    expect(selection.kind).toBe("skill");
    if (selection.kind === "skill") {
      expect(selection.match.skill.id).toBe("skill.promoted.public-status-scraper");
      expect(selection.match.score).toBeGreaterThanOrEqual(skillReuseThreshold);
    }
  });

  it("rejects invalid promoted skill metadata", () => {
    expect(() =>
      registerPromotedSkill({
        ...promotedScraperSkill(),
        id: "skill.not-promoted.public-status-scraper"
      })
    ).toThrow("Promoted skill ids");
  });

  it("throws for unknown required skills", () => {
    expect(() => requireSkill("skill.missing")).toThrow("No deterministic skill matched");
  });
});

function promotedScraperSkill(): SkillMetadata {
  const runtimeTemplate = requireSkill("skill.gmail.receipts.read").runtimeTemplate;

  return {
    id: "skill.promoted.public-status-scraper",
    name: "Public Status Scraper",
    version: "1.0.0",
    description: "Scrapes a public status page using reviewed generated code.",
    deterministic: true,
    nodeKinds: ["skill"],
    capabilities: ["public-status-scrape"],
    inputSchema: {
      request: { type: "object", additionalProperties: true }
    },
    outputSchema: {
      page: { type: "object", additionalProperties: true }
    },
    requiredSecrets: [],
    adapterDependencies: [],
    adapterOperations: [],
    runtimeTemplate,
    metaprompt: "Select this skill for public status page scraping workflows.",
    validationRules: ["fixture output must contain page"],
    examples: [
      {
        id: "example.public-status-scraper",
        description: "Scrape incidents from a public status page.",
        input: { request: { url: "https://status.example.com" } },
        output: { page: { incidents: [] } }
      }
    ],
    source: "promoted",
    promotedFromNodeId: "scrape-status-page"
  };
}
