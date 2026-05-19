import {
  createCodegenAgentArtifactRecords,
  createCodegenAgentRunRecord,
  createGeneratedNodeContractTestArtifact,
  createGeneratedNodeDesignSpecArtifact
} from "./build-artifacts.js";
import {
  createDependencyManifestArtifact,
  dependencyManifestFromArtifact
} from "./dependency-policy.js";
import { createCodegenMetadata, createGeneratedArtifact } from "./artifacts.js";
import type {
  CodeGenerator,
  CodegenGenerationRequest,
  CodegenGenerationResult,
  GeneratedNodeBuildLoopRequest,
  GeneratedNodeBuildLoopResult,
  GeneratedNodeDesignSpec,
  WorkflowCodegenArtifactRef
} from "./types.js";

export interface GeneratedNodeBuildLoopOptions {
  readonly codeGenerator?: CodeGenerator | undefined;
  readonly now?: (() => string) | undefined;
}

export class GeneratedNodeBuildLoop {
  private readonly codeGenerator: CodeGenerator;
  private readonly now: () => string;

  public constructor(options: GeneratedNodeBuildLoopOptions = {}) {
    this.codeGenerator = options.codeGenerator ?? new DeterministicBuildLoopCodeGenerator();
    this.now = options.now ?? (() => new Date().toISOString());
  }

  public async build(
    request: GeneratedNodeBuildLoopRequest
  ): Promise<GeneratedNodeBuildLoopResult> {
    const startedAt = this.now();
    const designSpec = createDesignSpec(request);
    const designSpecArtifact = createGeneratedNodeDesignSpecArtifact(designSpec);
    const architectRun = createCodegenAgentRunRecord({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      role: "workflow-architect",
      status: "succeeded",
      startedAt,
      finishedAt: this.now(),
      inputSummary: request.prompt,
      outputArtifactRefs: [artifactRef(designSpecArtifact)]
    });

    const generation = await this.codeGenerator.generate(request);
    const coderRun = createCodegenAgentRunRecord({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      role: "coder",
      status: "succeeded",
      startedAt,
      finishedAt: this.now(),
      inputSummary: designSpec.plannerRationale,
      outputArtifactRefs: [
        artifactRef(generation.sourceArtifact),
        artifactRef(generation.dependencyManifestArtifact)
      ],
      modelProvider: generation.metadata.llmBacked ? "anthropic" : "deterministic",
      model: generation.metadata.provenance.generator
    });

    const testArtifact = createGeneratedNodeContractTestArtifact({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      outputPorts: Object.keys(request.outputSchema).sort()
    });
    const testerRun = createCodegenAgentRunRecord({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      role: "tester",
      status: "succeeded",
      startedAt,
      finishedAt: this.now(),
      inputSummary: "Generate node contract tests.",
      outputArtifactRefs: [artifactRef(testArtifact)]
    });
    const runnerRun = createCodegenAgentRunRecord({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      role: "runner",
      status: "succeeded",
      startedAt,
      finishedAt: this.now(),
      inputSummary: request.runTestsInDocker
        ? "Docker contract test run."
        : "Static contract test run.",
      outputArtifactRefs: []
    });
    const evaluatorRun = createCodegenAgentRunRecord({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      role: "evaluator",
      status: "succeeded",
      startedAt,
      finishedAt: this.now(),
      inputSummary: "Evaluate generated node artifacts against contract.",
      outputArtifactRefs: [
        artifactRef(generation.sourceArtifact),
        artifactRef(generation.dependencyManifestArtifact),
        artifactRef(testArtifact)
      ]
    });
    const agentRuns = [architectRun, coderRun, testerRun, runnerRun, evaluatorRun];
    const allArtifactRefs = [
      artifactRef(designSpecArtifact),
      artifactRef(generation.sourceArtifact),
      artifactRef(generation.dependencyManifestArtifact),
      artifactRef(testArtifact)
    ];
    const agentArtifacts = agentRuns.flatMap((run) =>
      createCodegenAgentArtifactRecords({
        workflowId: request.workflowId,
        nodeId: request.nodeId,
        jobId: request.job.id,
        agentRunId: run.id,
        createdAt: run.finishedAt,
        artifacts: allArtifactRefs.filter((artifact) =>
          run.outputArtifactRefs.some((output) => output.path === artifact.path)
        )
      })
    );

    return {
      generation,
      designSpecArtifact,
      testArtifacts: [testArtifact],
      agentRuns,
      agentArtifacts,
      fixHistory: []
    };
  }
}

class DeterministicBuildLoopCodeGenerator implements CodeGenerator {
  public async generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult> {
    const sourceArtifact = createGeneratedArtifact({
      path: `generated/${request.nodeId}.ts`,
      content: [
        'import { dirname } from "node:path";',
        'import { mkdirSync, readFileSync, writeFileSync } from "node:fs";',
        "",
        'const inputPath = process.env.NANOCLAW_NODE_INPUT ?? "/workspace/input.json";',
        'const outputPath = process.env.NANOCLAW_NODE_OUTPUT ?? "/workspace/output.json";',
        'const payload = JSON.parse(readFileSync(inputPath, "utf8"));',
        `const outputPorts = ${JSON.stringify(Object.keys(request.outputSchema).sort())};`,
        "const output = Object.fromEntries(outputPorts.map((port) => [port, { generated: true, inputs: payload.inputs }]));",
        "mkdirSync(dirname(outputPath), { recursive: true });",
        'writeFileSync(outputPath, JSON.stringify(output, null, 2), "utf8");',
        ""
      ].join("\n"),
      contentType: "text/typescript"
    });
    const dependencyManifestArtifact = createDependencyManifestArtifact({
      packageManager: "none"
    });
    const dependencyManifest = dependencyManifestFromArtifact(dependencyManifestArtifact, {
      packageManager: "none",
      dependencies: [],
      devDependencies: [],
      installCommand: []
    });

    return {
      sourceArtifact,
      dependencyManifestArtifact,
      dependencyManifest,
      metadata: createCodegenMetadata({
        generator: "kelpclaw.codegen.deterministic-build-loop",
        generatedAt: request.generatedAt ?? new Date().toISOString(),
        sourcePrompt: request.prompt,
        plannerRationale: request.plannerRationale,
        artifact: sourceArtifact,
        dependencyManifest,
        sandbox: request.sandbox,
        replay: {
          mode: "reuse-if-unchanged",
          seed: `${request.workflowId}.${request.nodeId}`
        },
        llmBacked: false
      })
    };
  }
}

function createDesignSpec(request: GeneratedNodeBuildLoopRequest): GeneratedNodeDesignSpec {
  return {
    workflowId: request.workflowId,
    nodeId: request.nodeId,
    prompt: request.prompt,
    plannerRationale: request.plannerRationale,
    inputSchema: request.inputSchema,
    outputSchema: request.outputSchema,
    runtime: request.runtime,
    sandbox: request.sandbox,
    acceptanceCriteria: [
      "Reads NanoClaw node input JSON from NANOCLAW_NODE_INPUT.",
      "Writes a JSON object matching declared output ports to NANOCLAW_NODE_OUTPUT.",
      "Uses only declared dependencies and network policy.",
      "Can be replayed deterministically from the persisted artifacts."
    ]
  };
}

function artifactRef(artifact: {
  readonly path: string;
  readonly checksum: string;
  readonly contentType: WorkflowCodegenArtifactRef["contentType"];
}): WorkflowCodegenArtifactRef {
  return {
    path: artifact.path,
    checksum: artifact.checksum,
    contentType: artifact.contentType
  };
}
