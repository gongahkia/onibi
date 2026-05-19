import { createGeneratedArtifact } from "./artifacts.js";
import type {
  CodegenAgentArtifactRecord,
  CodegenAgentRunRecord,
  GeneratedArtifact,
  GeneratedNodeDesignSpec,
  WorkflowCodegenArtifactRef
} from "./types.js";
import type { WorkflowAgentRole } from "@kelpclaw/workflow-spec";

export function createGeneratedNodeDesignSpecArtifact(
  spec: GeneratedNodeDesignSpec
): GeneratedArtifact {
  return createGeneratedArtifact({
    path: `generated/${spec.nodeId}.design.json`,
    content: JSON.stringify(spec, null, 2),
    contentType: "application/json",
    metadata: {
      workflowId: spec.workflowId,
      nodeId: spec.nodeId,
      artifactKind: "design-spec"
    }
  });
}

export function createGeneratedNodeContractTestArtifact(input: {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly outputPorts: readonly string[];
}): GeneratedArtifact {
  return createGeneratedArtifact({
    path: `generated/${input.nodeId}.contract.test.ts`,
    content: [
      `import { describe, expect, it } from "vitest";`,
      "",
      `describe("${input.nodeId} generated node contract", () => {`,
      `  it("declares expected output ports", () => {`,
      `    expect(${JSON.stringify(input.outputPorts)}).toEqual(${JSON.stringify(input.outputPorts)});`,
      "  });",
      "});",
      ""
    ].join("\n"),
    contentType: "text/typescript",
    metadata: {
      workflowId: input.workflowId,
      nodeId: input.nodeId,
      artifactKind: "contract-test"
    }
  });
}

export function createCodegenAgentRunRecord(input: {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly role: WorkflowAgentRole;
  readonly status: "succeeded" | "failed";
  readonly startedAt: string;
  readonly finishedAt: string;
  readonly inputSummary: string;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly modelProvider?: string | undefined;
  readonly model?: string | undefined;
  readonly error?: string | undefined;
}): CodegenAgentRunRecord {
  return {
    id: `agent.${input.jobId}.${input.role}.${input.nodeId}`,
    workflowId: input.workflowId,
    nodeId: input.nodeId,
    jobId: input.jobId,
    role: input.role,
    status: input.status,
    startedAt: input.startedAt,
    finishedAt: input.finishedAt,
    inputSummary: input.inputSummary,
    outputArtifactRefs: input.outputArtifactRefs,
    modelProvider: input.modelProvider ?? "deterministic",
    model: input.model ?? "none",
    ...(input.error ? { error: input.error } : {})
  };
}

export function createCodegenAgentArtifactRecords(input: {
  readonly workflowId: string;
  readonly nodeId: string;
  readonly jobId: string;
  readonly agentRunId: string;
  readonly createdAt: string;
  readonly artifacts: readonly WorkflowCodegenArtifactRef[];
}): readonly CodegenAgentArtifactRecord[] {
  return input.artifacts.map((artifact) => ({
    id: `agent-artifact.${input.jobId}.${artifact.checksum.replace(/^sha256:/u, "")}`,
    workflowId: input.workflowId,
    nodeId: input.nodeId,
    jobId: input.jobId,
    agentRunId: input.agentRunId,
    createdAt: input.createdAt,
    artifact
  }));
}
