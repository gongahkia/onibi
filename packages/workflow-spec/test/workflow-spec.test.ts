import { describe, expect, it } from "vitest";
import {
  cyclicWorkflowFixture,
  missingEdgeTargetWorkflowFixture,
  stableWorkflowStringify,
  staticContentWorkflowFixture,
  validateWorkflowSpec,
  workflowJsonSchema
} from "../src/index.js";

describe("workflow spec validation", () => {
  it("accepts a valid deterministic workflow fixture", () => {
    const result = validateWorkflowSpec(staticContentWorkflowFixture);

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.workflow.metadata.id).toBe("workflow.static-content");
    }
  });

  it("rejects missing edge targets with stable error codes", () => {
    const result = validateWorkflowSpec(missingEdgeTargetWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual(["WORKFLOW_EDGE_TARGET_MISSING"]);
    }
  });

  it("rejects cyclic graphs with a stable error code", () => {
    const result = validateWorkflowSpec(cyclicWorkflowFixture);

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual(["WORKFLOW_DAG_CYCLE"]);
    }
  });

  it("rejects duplicate node ids with a stable error code", () => {
    const result = validateWorkflowSpec({
      ...staticContentWorkflowFixture,
      nodes: [
        staticContentWorkflowFixture.nodes[0],
        { ...staticContentWorkflowFixture.nodes[0], label: "Duplicate" }
      ],
      edges: []
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.errors.map((error) => error.code)).toEqual(["WORKFLOW_NODE_ID_DUPLICATE"]);
    }
  });

  it("stringifies workflow specs with stable key and collection ordering", () => {
    const shuffled = {
      ...staticContentWorkflowFixture,
      nodes: [...staticContentWorkflowFixture.nodes].reverse(),
      edges: [...staticContentWorkflowFixture.edges].reverse()
    };

    expect(stableWorkflowStringify(shuffled)).toBe(
      stableWorkflowStringify(staticContentWorkflowFixture)
    );
  });

  it("exports a JSON Schema document for generated workflow specs", () => {
    expect(workflowJsonSchema.$id).toBe("https://kelpclaw.dev/schemas/workflow-spec.schema.json");
    expect(workflowJsonSchema.required).toEqual(["metadata", "nodes", "edges"]);
  });
});
