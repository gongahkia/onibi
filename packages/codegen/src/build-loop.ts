import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, isAbsolute, relative, resolve } from "node:path";
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
  CodegenAgentRunRecord,
  CodegenGenerationRequest,
  CodegenGenerationResult,
  DockerGeneratedNodeCommand,
  DockerGeneratedNodeCommandResult,
  DockerGeneratedNodeCommandRunner,
  GeneratedArtifact,
  GeneratedNodeBuildLoopRequest,
  GeneratedNodeBuildLoopResult,
  GeneratedNodeBuildRole,
  GeneratedNodeDesignSpec,
  GeneratedNodeFixTriageDecision,
  GeneratedNodeRoleRunInput,
  GeneratedNodeRoleRunResult,
  GeneratedNodeRoleRunner,
  GeneratedNodeTestExecution,
  GeneratedNodeTestExecutor,
  WorkflowCodegenArtifactRef
} from "./types.js";

export interface GeneratedNodeBuildLoopOptions {
  readonly codeGenerator?: CodeGenerator | undefined;
  readonly roleRunners?:
    | Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>
    | undefined;
  readonly testExecutor?: GeneratedNodeTestExecutor | undefined;
  readonly workspaceRoot?: string | undefined;
  readonly now?: (() => string) | undefined;
}

export class GeneratedNodeBuildLoop {
  private readonly codeGenerator: CodeGenerator;
  private readonly roleRunners: Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>;
  private readonly testExecutor: GeneratedNodeTestExecutor;
  private readonly workspaceRoot: string | undefined;
  private readonly now: () => string;

  public constructor(options: GeneratedNodeBuildLoopOptions = {}) {
    this.codeGenerator = options.codeGenerator ?? new DeterministicBuildLoopCodeGenerator();
    this.roleRunners = options.roleRunners ?? {};
    this.testExecutor = options.testExecutor ?? new DefaultGeneratedNodeTestExecutor();
    this.workspaceRoot = options.workspaceRoot;
    this.now = options.now ?? (() => new Date().toISOString());
  }

  public async build(
    request: GeneratedNodeBuildLoopRequest
  ): Promise<GeneratedNodeBuildLoopResult> {
    const startedAt = this.now();
    const deadline = Date.now() + request.maxWallClockSeconds * 1000;
    const designSpec = createDesignSpec(request);
    const designSpecArtifact = createGeneratedNodeDesignSpecArtifact(designSpec);
    const testArtifacts = [
      createGeneratedNodeContractTestArtifact({
        workflowId: request.workflowId,
        nodeId: request.nodeId,
        outputPorts: Object.keys(request.outputSchema).sort()
      }),
      createGeneratedNodeFixtureTestArtifact(request)
    ];
    const agentRuns: CodegenAgentRunRecord[] = [];
    const fixHistory: string[] = [];
    const logs: string[] = [];
    let spentModelCostUsd = 0;
    let lastGeneration: CodegenGenerationResult | undefined;
    let lastExecution: GeneratedNodeTestExecution | undefined;
    let lastFailure: string | undefined;
    let nextCoderInputSummary = request.plannerRationale;
    let rearchitectBeforeNextCoder = false;
    let reimplementationAttempts = 0;
    const maxReimplementationAttempts = request.maxReimplementationAttempts ?? 2;

    await materializeWorkspaceFiles(this.workspaceRootFor(request), [
      designSpecArtifact,
      ...testArtifacts
    ]);

    const architectResult = await this.runRole({
      role: "workflow-architect",
      request,
      iteration: 0,
      inputSummary: request.prompt,
      outputArtifactRefs: [artifactRef(designSpecArtifact)],
      generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
    });
    spentModelCostUsd += architectResult.modelCostUsd ?? 0;
    agentRuns.push(
      runToRecord(request, "workflow-architect", architectResult, startedAt, this.now())
    );

    for (let iteration = 1; iteration <= request.maxIterations; iteration += 1) {
      throwIfAborted(request);
      if (Date.now() > deadline) {
        lastFailure = `Generated-node build exceeded ${request.maxWallClockSeconds}s wall-clock budget.`;
        break;
      }
      if (spentModelCostUsd > request.maxModelCostUsd) {
        lastFailure = `Generated-node build exceeded $${request.maxModelCostUsd} model budget.`;
        break;
      }

      if (rearchitectBeforeNextCoder) {
        const budgetFailure = modelBudgetFailureForRole(
          request,
          "workflow-architect",
          spentModelCostUsd
        );
        if (budgetFailure) {
          lastFailure = budgetFailure;
          break;
        }
        reimplementationAttempts += 1;
        if (reimplementationAttempts > maxReimplementationAttempts) {
          lastFailure = `Generated-node reimplementation loop exceeded ${maxReimplementationAttempts} rearchitecture attempt${maxReimplementationAttempts === 1 ? "" : "s"}.`;
          fixHistory.push(
            `reimplementation threshold: attempted ${reimplementationAttempts}; limit ${maxReimplementationAttempts}`
          );
          break;
        }
        const architectResult = await this.runRole({
          role: "workflow-architect",
          request,
          iteration,
          inputSummary: lastFailure ?? "Fixer triage requested a full generated-node redesign.",
          outputArtifactRefs: [
            artifactRef(designSpecArtifact),
            ...(lastGeneration ? [artifactRef(lastGeneration.sourceArtifact)] : [])
          ],
          previousFailure: lastFailure,
          generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
        });
        spentModelCostUsd += architectResult.modelCostUsd ?? 0;
        agentRuns.push(
          runToRecord(request, "workflow-architect", architectResult, startedAt, this.now())
        );
        nextCoderInputSummary = [
          "Rearchitect generated node from scratch after fixer triage.",
          architectResult.inputSummary,
          lastFailure ? `Prior failure: ${lastFailure}` : ""
        ]
          .filter((part) => part.length > 0)
          .join(" ");
        rearchitectBeforeNextCoder = false;
      }

      const coderBudgetFailure = modelBudgetFailureForRole(request, "coder", spentModelCostUsd);
      if (coderBudgetFailure) {
        lastFailure = coderBudgetFailure;
        break;
      }
      const coderResult = await this.runRole({
        role: "coder",
        request,
        iteration,
        inputSummary: nextCoderInputSummary,
        outputArtifactRefs: [],
        previousFailure: lastFailure,
        generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
      });
      spentModelCostUsd += coderResult.modelCostUsd ?? 0;
      agentRuns.push(runToRecord(request, "coder", coderResult, startedAt, this.now()));
      if (coderResult.generation) {
        lastGeneration = coderResult.generation;
        await materializeWorkspaceFiles(this.workspaceRootFor(request), [
          lastGeneration.sourceArtifact,
          lastGeneration.dependencyManifestArtifact
        ]);
      }
      if (coderResult.status === "failed" || !lastGeneration) {
        lastFailure = coderResult.error ?? "Coder role did not produce generated node artifacts.";
        if (iteration < request.maxIterations) {
          const fix = await this.runFixerTriage({
            request,
            iteration,
            failure: lastFailure,
            outputArtifactRefs: [],
            startedAt,
            agentRuns,
            spentModelCostUsd
          });
          spentModelCostUsd += fix.modelCostUsd;
          const decision = fix.decision;
          fixHistory.push(
            `iteration ${iteration}: ${lastFailure}; fixer triage: ${formatFixTriageDecision(decision)}`
          );
          if (decision.action === "give-up") {
            break;
          }
          rearchitectBeforeNextCoder = decision.action === "rearchitect";
          nextCoderInputSummary = coderInputSummaryFromFixer(decision, lastFailure);
          continue;
        }
        fixHistory.push(`iteration ${iteration}: ${lastFailure}`);
        break;
      }

      const testerBudgetFailure = modelBudgetFailureForRole(request, "tester", spentModelCostUsd);
      if (testerBudgetFailure) {
        lastFailure = testerBudgetFailure;
        break;
      }
      const testerResult = await this.runRole({
        role: "tester",
        request,
        iteration,
        inputSummary: "Generate contract and workflow fixture tests.",
        outputArtifactRefs: testArtifacts.map(artifactRef),
        previousFailure: lastFailure,
        generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
      });
      spentModelCostUsd += testerResult.modelCostUsd ?? 0;
      agentRuns.push(runToRecord(request, "tester", testerResult, startedAt, this.now()));

      const runnerInput = {
        request,
        generation: lastGeneration,
        testArtifacts,
        iteration
      };
      lastExecution = await this.testExecutor.execute(runnerInput);
      logs.push(...lastExecution.logs);
      const runnerBudgetFailure = modelBudgetFailureForRole(request, "runner", spentModelCostUsd);
      if (runnerBudgetFailure) {
        lastFailure = runnerBudgetFailure;
        break;
      }
      const runnerResult = await this.runRole({
        role: "runner",
        request,
        iteration,
        inputSummary: request.runTestsInDocker
          ? "Docker scoped generated-node test execution."
          : "Static scoped generated-node test execution.",
        outputArtifactRefs: lastExecution.resultArtifacts.map(artifactRef),
        previousFailure: lastExecution.failureMessage,
        generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
      });
      spentModelCostUsd += runnerResult.modelCostUsd ?? 0;
      agentRuns.push(runToRecord(request, "runner", runnerResult, startedAt, this.now()));

      const evaluatorBudgetFailure = modelBudgetFailureForRole(
        request,
        "evaluator",
        spentModelCostUsd
      );
      if (evaluatorBudgetFailure) {
        lastFailure = evaluatorBudgetFailure;
        break;
      }
      const evaluatorResult = await this.runRole({
        role: "evaluator",
        request,
        iteration,
        inputSummary: lastExecution.failureMessage ?? "Evaluate generated node artifacts.",
        outputArtifactRefs: [
          artifactRef(lastGeneration.sourceArtifact),
          artifactRef(lastGeneration.dependencyManifestArtifact),
          ...lastExecution.resultArtifacts.map(artifactRef)
        ],
        previousFailure: lastExecution.failureMessage,
        generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
      });
      spentModelCostUsd += evaluatorResult.modelCostUsd ?? 0;
      agentRuns.push(runToRecord(request, "evaluator", evaluatorResult, startedAt, this.now()));

      if (lastExecution.status === "passed") {
        const allArtifactRefs = [
          artifactRef(designSpecArtifact),
          artifactRef(lastGeneration.sourceArtifact),
          artifactRef(lastGeneration.dependencyManifestArtifact),
          ...testArtifacts.map(artifactRef),
          ...lastExecution.resultArtifacts.map(artifactRef)
        ];
        return {
          status: "passed",
          generation: lastGeneration,
          designSpecArtifact,
          testArtifacts,
          resultArtifacts: lastExecution.resultArtifacts,
          agentRuns,
          agentArtifacts: createAgentArtifacts(request, agentRuns, allArtifactRefs),
          fixHistory,
          logs,
          schemaValid: lastExecution.schemaValid,
          securityValid: lastExecution.securityValid,
          replayValid: lastExecution.replayValid,
          dependencyPolicyValid: lastExecution.dependencyPolicyValid,
          findings: lastExecution.findings
        };
      }

      lastFailure = lastExecution.failureMessage ?? "Generated-node eval failed.";
      if (iteration < request.maxIterations) {
        const fix = await this.runFixerTriage({
          request,
          iteration,
          failure: lastFailure,
          outputArtifactRefs: [
            artifactRef(lastGeneration.sourceArtifact),
            artifactRef(lastGeneration.dependencyManifestArtifact),
            ...lastExecution.resultArtifacts.map(artifactRef)
          ],
          startedAt,
          agentRuns,
          spentModelCostUsd
        });
        spentModelCostUsd += fix.modelCostUsd;
        const decision = fix.decision;
        fixHistory.push(
          `iteration ${iteration}: ${lastFailure}; fixer triage: ${formatFixTriageDecision(decision)}`
        );
        if (decision.action === "give-up") {
          break;
        }
        rearchitectBeforeNextCoder = decision.action === "rearchitect";
        nextCoderInputSummary = coderInputSummaryFromFixer(decision, lastFailure);
      } else {
        fixHistory.push(`iteration ${iteration}: ${lastFailure}`);
      }
    }

    const failureGeneration =
      lastGeneration ?? createFailureGeneration(request, lastFailure ?? "Code generation failed.");
    const unresolvedFailureArtifact = createUnresolvedFailureArtifact(request, {
      failure: lastFailure ?? "Generated-node build loop did not pass before the budget expired.",
      fixHistory
    });
    await materializeWorkspaceFiles(this.workspaceRootFor(request), [
      failureGeneration.sourceArtifact,
      failureGeneration.dependencyManifestArtifact,
      unresolvedFailureArtifact
    ]);
    const failedExecution =
      lastExecution ??
      createFailedExecution(
        request,
        unresolvedFailureArtifact,
        lastFailure ?? "Generated-node build loop did not produce an executable candidate."
      );
    const allArtifactRefs = [
      artifactRef(designSpecArtifact),
      artifactRef(failureGeneration.sourceArtifact),
      artifactRef(failureGeneration.dependencyManifestArtifact),
      ...testArtifacts.map(artifactRef),
      ...failedExecution.resultArtifacts.map(artifactRef),
      artifactRef(unresolvedFailureArtifact)
    ];

    return {
      status: "failed",
      generation: failureGeneration,
      designSpecArtifact,
      testArtifacts,
      resultArtifacts: failedExecution.resultArtifacts,
      agentRuns,
      agentArtifacts: createAgentArtifacts(request, agentRuns, allArtifactRefs),
      fixHistory,
      logs: [...logs, ...(failedExecution.logs.length ? failedExecution.logs : [])],
      schemaValid: failedExecution.schemaValid,
      securityValid: failedExecution.securityValid,
      replayValid: failedExecution.replayValid,
      dependencyPolicyValid: failedExecution.dependencyPolicyValid,
      findings: failedExecution.findings,
      unresolvedFailureArtifact
    };
  }

  private async runRole(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult> {
    throwIfAborted(input.request);
    const runner =
      this.roleRunners[input.role] ?? new DeterministicGeneratedNodeRoleRunner(input.role);
    const result = await runner.run(input);
    throwIfAborted(input.request);
    return result;
  }

  private async runFixerTriage(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly iteration: number;
    readonly failure: string;
    readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
    readonly startedAt: string;
    readonly agentRuns: CodegenAgentRunRecord[];
    readonly spentModelCostUsd: number;
  }): Promise<{
    readonly decision: GeneratedNodeFixTriageDecision;
    readonly modelCostUsd: number;
  }> {
    const budgetFailure = modelBudgetFailureForRole(
      input.request,
      "fixer",
      input.spentModelCostUsd
    );
    if (budgetFailure) {
      const decision: GeneratedNodeFixTriageDecision = {
        action: "give-up",
        scope: "external-blocker",
        rationale: budgetFailure,
        confidence: 1
      };
      return { decision, modelCostUsd: 0 };
    }
    const result = await this.runRole({
      role: "fixer",
      request: input.request,
      iteration: input.iteration,
      inputSummary: [
        "Diagnose before repairing.",
        "Classify whether this is a small local fix, a normal regeneration, a workflow redesign, or an external blocker.",
        input.failure
      ].join(" "),
      outputArtifactRefs: input.outputArtifactRefs,
      previousFailure: input.failure,
      generateCode: (codegenRequest) => this.codeGenerator.generate(codegenRequest)
    });
    input.agentRuns.push(runToRecord(input.request, "fixer", result, input.startedAt, this.now()));

    return {
      decision: result.fixTriage ?? inferFixTriageDecision(input.failure),
      modelCostUsd: result.modelCostUsd ?? 0
    };
  }

  private workspaceRootFor(request: GeneratedNodeBuildLoopRequest): string | undefined {
    return request.workspaceRoot ?? this.workspaceRoot;
  }
}

class DeterministicGeneratedNodeRoleRunner implements GeneratedNodeRoleRunner {
  public constructor(public readonly role: GeneratedNodeBuildRole) {}

  public async run(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult> {
    if (input.role === "coder") {
      try {
        const generation = await input.generateCode(input.request);
        return {
          status: "succeeded",
          inputSummary: input.inputSummary,
          outputArtifactRefs: [
            artifactRef(generation.sourceArtifact),
            artifactRef(generation.dependencyManifestArtifact)
          ],
          generation,
          modelProvider: generation.metadata.llmBacked ? "anthropic" : "deterministic",
          model: generation.metadata.provenance.generator,
          modelInvocations: generation.metadata.llmBacked
            ? [
                createModelInvocationRecord(input, {
                  provider: "anthropic",
                  model: generation.metadata.provenance.generator,
                  outputArtifact: generation.sourceArtifact.path
                })
              ]
            : []
        };
      } catch (error) {
        return {
          status: "failed",
          inputSummary: input.inputSummary,
          outputArtifactRefs: [],
          error: error instanceof Error ? error.message : "Code generation failed."
        };
      }
    }

    if (input.role === "fixer") {
      const decision = inferFixTriageDecision(input.previousFailure ?? input.inputSummary);
      return {
        status: decision.action === "give-up" ? "failed" : "succeeded",
        inputSummary: `Fixer triage selected ${formatFixTriageDecision(decision)}`,
        outputArtifactRefs: input.outputArtifactRefs,
        fixTriage: decision,
        ...(decision.action === "give-up" ? { error: decision.rationale } : {})
      };
    }

    return {
      status: "succeeded",
      inputSummary: input.inputSummary,
      outputArtifactRefs: input.outputArtifactRefs
    };
  }
}

function inferFixTriageDecision(failure: string): GeneratedNodeFixTriageDecision {
  const normalized = failure.toLowerCase();
  if (
    /\b(workflow design|planner|graph|edge|trigger|wrong node|wrong workflow|cannot satisfy)\b/u.test(
      normalized
    )
  ) {
    return {
      action: "rearchitect",
      scope: "workflow-design",
      rationale:
        "The failure appears to come from the generated-node design or surrounding workflow assumptions.",
      confidence: 0.8
    };
  }

  if (
    /\b(secret|credential|approval|quota|budget|policy review|external provider|permission)\b/u.test(
      normalized
    )
  ) {
    return {
      action: "give-up",
      scope: "external-blocker",
      rationale: "The failure appears blocked by external readiness rather than code generation.",
      confidence: 0.75
    };
  }

  if (
    /\b(schema|payload|contract|declared output|output port|input\/output|network|timeout|exit code)\b/u.test(
      normalized
    )
  ) {
    return {
      action: "targeted-patch",
      scope: "local-code",
      rationale:
        "The failure is localized to generated source behavior, payload shape, sandbox, or runtime wiring.",
      confidence: 0.82
    };
  }

  return {
    action: "retry-codegen",
    scope: "node-contract",
    rationale:
      "The failure is likely repairable by regenerating the node against the same contract.",
    confidence: 0.6
  };
}

function formatFixTriageDecision(decision: GeneratedNodeFixTriageDecision): string {
  return `${decision.action}/${decision.scope} (${Math.round(
    decision.confidence * 100
  )}%): ${decision.rationale}`;
}

function modelBudgetFailureForRole(
  request: GeneratedNodeBuildLoopRequest,
  role: GeneratedNodeBuildRole,
  spentModelCostUsd: number
): string | undefined {
  const projectedCostUsd = estimatedRoleModelCostUsd(role);
  if (spentModelCostUsd + projectedCostUsd <= request.maxModelCostUsd) {
    return undefined;
  }

  return `Generated-node build stopped before ${role} because projected next-step cost $${projectedCostUsd.toFixed(
    4
  )} would exceed remaining model budget $${Math.max(
    0,
    request.maxModelCostUsd - spentModelCostUsd
  ).toFixed(4)}.`;
}

function estimatedRoleModelCostUsd(role: GeneratedNodeBuildRole): number {
  switch (role) {
    case "workflow-architect":
    case "coder":
      return 0.25;
    case "fixer":
    case "evaluator":
      return 0.1;
    case "tester":
    case "runner":
      return 0.05;
  }
}

function coderInputSummaryFromFixer(
  decision: GeneratedNodeFixTriageDecision,
  failure: string
): string {
  switch (decision.action) {
    case "targeted-patch":
      return `Apply the smallest local patch. Do not redesign the workflow. Failure: ${failure}. Triage rationale: ${decision.rationale}`;
    case "retry-codegen":
      return `Regenerate the candidate against the existing node contract. Failure: ${failure}. Triage rationale: ${decision.rationale}`;
    case "rearchitect":
      return `Rebuild from a revised architecture before coding. Failure: ${failure}. Triage rationale: ${decision.rationale}`;
    case "give-up":
      return `Do not continue codegen. External blocker: ${failure}. Triage rationale: ${decision.rationale}`;
  }
}

export class DefaultGeneratedNodeTestExecutor implements GeneratedNodeTestExecutor {
  private readonly staticExecutor: GeneratedNodeTestExecutor;
  private readonly dockerExecutor: GeneratedNodeTestExecutor;

  public constructor(
    options: {
      readonly staticExecutor?: GeneratedNodeTestExecutor | undefined;
      readonly dockerExecutor?: GeneratedNodeTestExecutor | undefined;
    } = {}
  ) {
    this.staticExecutor = options.staticExecutor ?? new StaticGeneratedNodeTestExecutor();
    this.dockerExecutor = options.dockerExecutor ?? new DockerGeneratedNodeTestExecutor();
  }

  public async execute(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly generation: CodegenGenerationResult;
    readonly testArtifacts: readonly GeneratedArtifact[];
    readonly iteration: number;
  }): Promise<GeneratedNodeTestExecution> {
    return input.request.runTestsInDocker
      ? this.dockerExecutor.execute(input)
      : this.staticExecutor.execute(input);
  }
}

export class StaticGeneratedNodeTestExecutor implements GeneratedNodeTestExecutor {
  public async execute(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly generation: CodegenGenerationResult;
    readonly testArtifacts: readonly GeneratedArtifact[];
    readonly iteration: number;
  }): Promise<GeneratedNodeTestExecution> {
    throwIfAborted(input.request);
    const logs = [
      `Materialized generated node candidate for iteration ${input.iteration}.`,
      input.request.runTestsInDocker
        ? `Docker eval requested with network '${input.request.sandbox.network}'.`
        : "Static generated-node eval executed in scoped workspace."
    ];
    const findings = [];
    const source = input.generation.sourceArtifact.content;
    const outputPorts = Object.keys(input.request.outputSchema).sort();
    const schemaValid =
      outputPorts.length > 0 &&
      source.includes("NANOCLAW_NODE_INPUT") &&
      source.includes("NANOCLAW_NODE_OUTPUT");
    if (!schemaValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.schema`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: "Generated node does not satisfy the NanoClaw input/output payload contract.",
        issues: []
      });
    }
    const securityValid =
      input.request.sandbox.network !== "none" ||
      !/\b(fetch|XMLHttpRequest|https?:\/\/|node:net|node:http|node:https)\b/u.test(source);
    if (!securityValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.network`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: "Generated node attempted undeclared network access.",
        issues: []
      });
    }
    const dependencyPolicyValid = input.generation.dependencyManifest.packageManager === "none";
    if (!dependencyPolicyValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.dependencies`,
        severity: "error" as const,
        target: { kind: "artifact" as const, id: input.generation.dependencyManifest.path },
        message: "Generated node dependency manifest requires policy review.",
        issues: []
      });
    }
    const replayValid = input.generation.metadata.replay.mode !== "always-regenerate";
    const outputArtifact = createGeneratedArtifact({
      path: `generated/${input.request.nodeId}.eval-output.json`,
      content: JSON.stringify(
        {
          iteration: input.iteration,
          output: Object.fromEntries(
            outputPorts.map((port) => [port, { generated: true, fixture: true }])
          )
        },
        null,
        2
      ),
      contentType: "application/json"
    });
    await materializeWorkspaceFiles(input.request.workspaceRoot, [
      ...input.testArtifacts,
      outputArtifact
    ]);
    const passed = schemaValid && securityValid && dependencyPolicyValid && replayValid;

    return {
      status: passed ? "passed" : "failed",
      logs,
      resultArtifacts: [outputArtifact],
      schemaValid,
      securityValid,
      replayValid,
      dependencyPolicyValid,
      findings,
      ...(passed
        ? {}
        : {
            failureMessage: findings.map((finding) => finding.message).join("; ")
          })
    };
  }
}

export interface DockerGeneratedNodeTestExecutorOptions {
  readonly dockerBin?: string | undefined;
  readonly containerWorkspace?: string | undefined;
  readonly commandRunner?: DockerGeneratedNodeCommandRunner | undefined;
}

export class DockerGeneratedNodeTestExecutor implements GeneratedNodeTestExecutor {
  private readonly dockerBin: string;
  private readonly containerWorkspace: string;
  private readonly commandRunner: DockerGeneratedNodeCommandRunner;

  public constructor(options: DockerGeneratedNodeTestExecutorOptions = {}) {
    this.dockerBin = options.dockerBin ?? "docker";
    this.containerWorkspace = options.containerWorkspace ?? "/workspace";
    this.commandRunner = options.commandRunner ?? new SpawnDockerGeneratedNodeCommandRunner();
  }

  public buildCommand(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly workspaceRoot: string;
    readonly runtimeScriptPath: string;
    readonly inputPath: string;
    readonly outputPath: string;
    readonly stdoutPath: string;
    readonly stderrPath: string;
  }): DockerGeneratedNodeCommand {
    const network = input.request.sandbox.network === "none" ? "none" : "bridge";
    const containerPath = (path: string) => `${this.containerWorkspace}/${path}`;

    return {
      executable: this.dockerBin,
      args: [
        "run",
        "--rm",
        "--network",
        network,
        "--cpus",
        input.request.runtime.resources.cpu,
        "--memory",
        `${input.request.runtime.resources.memoryMb}m`,
        "--volume",
        `${input.workspaceRoot}:${this.containerWorkspace}:rw`,
        "--workdir",
        this.containerWorkspace,
        "--env",
        `NANOCLAW_NODE_INPUT=${containerPath(input.inputPath)}`,
        "--env",
        `NANOCLAW_NODE_OUTPUT=${containerPath(input.outputPath)}`,
        "--env",
        `NANOCLAW_NODE_ID=${input.request.nodeId}`,
        input.request.runtime.image,
        "node",
        containerPath(input.runtimeScriptPath)
      ],
      workspaceRoot: input.workspaceRoot,
      network,
      timeoutMs:
        (input.request.maxDockerRuntimeSeconds ?? input.request.runtime.timeoutSeconds) * 1000,
      inputPath: input.inputPath,
      outputPath: input.outputPath,
      stdoutPath: input.stdoutPath,
      stderrPath: input.stderrPath
    };
  }

  public async execute(input: {
    readonly request: GeneratedNodeBuildLoopRequest;
    readonly generation: CodegenGenerationResult;
    readonly testArtifacts: readonly GeneratedArtifact[];
    readonly iteration: number;
  }): Promise<GeneratedNodeTestExecution> {
    throwIfAborted(input.request);
    const workspaceRoot = input.request.workspaceRoot;
    if (!workspaceRoot) {
      return failedDockerExecution(input, "Docker generated-node eval requires a workspace root.");
    }

    const runtimeScript = createGeneratedArtifact({
      path: `generated/${input.request.nodeId}.docker-runner.mjs`,
      content: input.generation.sourceArtifact.content,
      contentType: "text/typescript"
    });
    const inputArtifact = createGeneratedArtifact({
      path: `generated/${input.request.nodeId}.docker-input.json`,
      content: JSON.stringify(createFixturePayload(input.request), null, 2),
      contentType: "application/json"
    });
    const outputPath = `generated/${input.request.nodeId}.docker-output.json`;
    const stdoutPath = `generated/${input.request.nodeId}.docker-stdout.log`;
    const stderrPath = `generated/${input.request.nodeId}.docker-stderr.log`;
    const command = this.buildCommand({
      request: input.request,
      workspaceRoot,
      runtimeScriptPath: runtimeScript.path,
      inputPath: inputArtifact.path,
      outputPath,
      stdoutPath,
      stderrPath
    });
    const commandArtifact = createGeneratedArtifact({
      path: `generated/${input.request.nodeId}.docker-command.json`,
      content: JSON.stringify(
        {
          executable: command.executable,
          args: command.args,
          network: command.network,
          timeoutMs: command.timeoutMs
        },
        null,
        2
      ),
      contentType: "application/json"
    });

    await materializeWorkspaceFiles(workspaceRoot, [
      ...input.testArtifacts,
      runtimeScript,
      inputArtifact,
      commandArtifact
    ]);

    const result = await this.commandRunner.run(command, input.request.signal);
    throwIfAborted(input.request);
    const stdoutArtifact = createGeneratedArtifact({
      path: stdoutPath,
      content: result.stdout,
      contentType: "text/plain"
    });
    const stderrArtifact = createGeneratedArtifact({
      path: stderrPath,
      content: result.stderr,
      contentType: "text/plain"
    });
    const outputRead =
      result.output === undefined
        ? await readDockerOutput(workspaceRoot, outputPath)
        : validateDockerOutput(result.output);
    const outputArtifact = createGeneratedArtifact({
      path: outputPath,
      content: outputRead.raw,
      contentType: "application/json"
    });
    await materializeWorkspaceFiles(workspaceRoot, [
      stdoutArtifact,
      stderrArtifact,
      outputArtifact
    ]);

    const findings = [];
    const outputPorts = Object.keys(input.request.outputSchema).sort();
    const schemaValid =
      outputRead.ok &&
      outputPorts.every((port) => Object.prototype.hasOwnProperty.call(outputRead.output, port));
    if (!schemaValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.docker-schema`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: outputRead.ok
          ? "Docker generated-node eval output did not include all declared output ports."
          : outputRead.error,
        issues: []
      });
    }
    const dependencyPolicyValid = input.generation.dependencyManifest.packageManager === "none";
    if (!dependencyPolicyValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.docker-dependencies`,
        severity: "error" as const,
        target: { kind: "artifact" as const, id: input.generation.dependencyManifest.path },
        message:
          "Docker generated-node eval requires a dependency-free candidate unless dependencies are already vendored in the scoped workspace.",
        issues: []
      });
    }
    const securityValid = command.network === "none" || input.request.sandbox.network !== "none";
    if (!securityValid) {
      findings.push({
        id: `finding.${input.request.nodeId}.docker-network`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: "Docker generated-node eval attempted undeclared network access.",
        issues: []
      });
    }
    if (result.timedOut) {
      findings.push({
        id: `finding.${input.request.nodeId}.docker-timeout`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: `Docker generated-node eval exceeded ${command.timeoutMs}ms runtime budget.`,
        issues: []
      });
    }
    if (result.exitCode !== 0) {
      findings.push({
        id: `finding.${input.request.nodeId}.docker-exit`,
        severity: "error" as const,
        target: { kind: "node" as const, id: input.request.nodeId },
        message: `Docker generated-node eval exited with code ${result.exitCode}.`,
        issues: []
      });
    }
    const replayValid = input.generation.metadata.replay.mode !== "always-regenerate";
    const passed =
      result.exitCode === 0 &&
      !result.timedOut &&
      schemaValid &&
      securityValid &&
      replayValid &&
      dependencyPolicyValid;

    return {
      status: passed ? "passed" : "failed",
      logs: [
        `Docker generated-node eval executed in ${workspaceRoot}.`,
        `${command.executable} ${command.args.join(" ")}`,
        ...(result.stdout.length > 0 ? [`stdout: ${result.stdout}`] : []),
        ...(result.stderr.length > 0 ? [`stderr: ${result.stderr}`] : [])
      ],
      resultArtifacts: [commandArtifact, stdoutArtifact, stderrArtifact, outputArtifact],
      schemaValid,
      securityValid,
      replayValid,
      dependencyPolicyValid,
      findings,
      ...(passed
        ? {}
        : {
            failureMessage:
              findings.map((finding) => finding.message).join("; ") ||
              "Docker generated-node eval failed."
          })
    };
  }
}

class SpawnDockerGeneratedNodeCommandRunner implements DockerGeneratedNodeCommandRunner {
  public async run(
    command: DockerGeneratedNodeCommand,
    signal?: AbortSignal | undefined
  ): Promise<DockerGeneratedNodeCommandResult> {
    const controller = new AbortController();
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, command.timeoutMs);
    const abortFromParent = () => controller.abort(signal?.reason);
    signal?.addEventListener("abort", abortFromParent, { once: true });

    try {
      return await new Promise<DockerGeneratedNodeCommandResult>((resolve, reject) => {
        const child = spawn(command.executable, command.args, {
          cwd: command.workspaceRoot,
          stdio: ["ignore", "pipe", "pipe"],
          signal: controller.signal
        });
        const stdoutChunks: Buffer[] = [];
        const stderrChunks: Buffer[] = [];
        let settled = false;
        const settle = (result: DockerGeneratedNodeCommandResult): void => {
          if (!settled) {
            settled = true;
            resolve(result);
          }
        };
        child.stdout.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
        child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
        child.on("error", (error) => {
          if (timedOut || controller.signal.aborted) {
            settle({
              exitCode: 1,
              stdout: Buffer.concat(stdoutChunks).toString("utf8"),
              stderr:
                Buffer.concat(stderrChunks).toString("utf8") ||
                (error instanceof Error ? error.message : "Docker command aborted."),
              timedOut
            });
            return;
          }
          reject(error);
        });
        child.on("close", (code) =>
          settle({
            exitCode: code ?? 1,
            stdout: Buffer.concat(stdoutChunks).toString("utf8"),
            stderr: Buffer.concat(stderrChunks).toString("utf8"),
            ...(timedOut ? { timedOut } : {})
          })
        );
      });
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener("abort", abortFromParent);
    }
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

function createGeneratedNodeFixtureTestArtifact(
  request: GeneratedNodeBuildLoopRequest
): GeneratedArtifact {
  return createGeneratedArtifact({
    path: `generated/${request.nodeId}.fixture.test.json`,
    content: JSON.stringify(
      {
        workflowId: request.workflowId,
        nodeId: request.nodeId,
        input: Object.fromEntries(
          Object.keys(request.inputSchema)
            .sort()
            .map((port) => [port, { fixture: true }])
        ),
        expectedOutputPorts: Object.keys(request.outputSchema).sort()
      },
      null,
      2
    ),
    contentType: "application/json",
    metadata: {
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      artifactKind: "workflow-fixture-test"
    }
  });
}

function createFailureGeneration(
  request: GeneratedNodeBuildLoopRequest,
  failure: string
): CodegenGenerationResult {
  const sourceArtifact = createGeneratedArtifact({
    path: `generated/${request.nodeId}.failed.ts`,
    content: [
      'import { writeFileSync } from "node:fs";',
      'const outputPath = process.env.NANOCLAW_NODE_OUTPUT ?? "/workspace/output.json";',
      `writeFileSync(outputPath, ${JSON.stringify(JSON.stringify({ error: failure }, null, 2))});`,
      `throw new Error(${JSON.stringify(failure)});`,
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
      generator: "kelpclaw.codegen.unresolved-failure",
      generatedAt: request.generatedAt ?? new Date().toISOString(),
      sourcePrompt: request.prompt,
      plannerRationale: request.plannerRationale,
      artifact: sourceArtifact,
      dependencyManifest,
      sandbox: request.sandbox,
      replay: {
        mode: "fail-on-drift",
        seed: `${request.workflowId}.${request.nodeId}.failed`
      },
      llmBacked: false
    })
  };
}

function createFailedExecution(
  request: GeneratedNodeBuildLoopRequest,
  artifact: GeneratedArtifact,
  failure: string
): GeneratedNodeTestExecution {
  return {
    status: "failed",
    logs: [failure],
    resultArtifacts: [artifact],
    schemaValid: false,
    securityValid: false,
    replayValid: false,
    dependencyPolicyValid: false,
    findings: [
      {
        id: `finding.${request.nodeId}.unresolved`,
        severity: "error",
        target: { kind: "node", id: request.nodeId },
        message: failure,
        issues: []
      }
    ],
    failureMessage: failure
  };
}

function createUnresolvedFailureArtifact(
  request: GeneratedNodeBuildLoopRequest,
  input: { readonly failure: string; readonly fixHistory: readonly string[] }
): GeneratedArtifact {
  return createGeneratedArtifact({
    path: `generated/${request.nodeId}.unresolved-failure.json`,
    content: JSON.stringify(
      {
        workflowId: request.workflowId,
        nodeId: request.nodeId,
        failure: input.failure,
        fixHistory: input.fixHistory
      },
      null,
      2
    ),
    contentType: "application/json"
  });
}

function runToRecord(
  request: GeneratedNodeBuildLoopRequest,
  role: GeneratedNodeBuildRole,
  result: GeneratedNodeRoleRunResult,
  startedAt: string,
  finishedAt: string
) {
  return createCodegenAgentRunRecord({
    workflowId: request.workflowId,
    nodeId: request.nodeId,
    jobId: request.job.id,
    role,
    status: result.status,
    startedAt,
    finishedAt,
    inputSummary: result.inputSummary,
    outputArtifactRefs: result.outputArtifactRefs,
    modelProvider: result.modelProvider,
    model: result.model,
    modelInvocations: result.modelInvocations,
    error: result.error
  });
}

function createAgentArtifacts(
  request: GeneratedNodeBuildLoopRequest,
  agentRuns: readonly ReturnType<typeof createCodegenAgentRunRecord>[],
  artifactRefs: readonly WorkflowCodegenArtifactRef[]
) {
  return agentRuns.flatMap((run) =>
    createCodegenAgentArtifactRecords({
      workflowId: request.workflowId,
      nodeId: request.nodeId,
      jobId: request.job.id,
      agentRunId: run.id,
      createdAt: run.finishedAt,
      artifacts: artifactRefs.filter((artifact) =>
        run.outputArtifactRefs.some((output) => output.path === artifact.path)
      )
    })
  );
}

async function materializeWorkspaceFiles(
  workspaceRoot: string | undefined,
  artifacts: readonly GeneratedArtifact[]
): Promise<void> {
  if (!workspaceRoot) {
    return;
  }
  for (const artifact of artifacts) {
    const path = resolveWorkspacePath(workspaceRoot, artifact.path);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path, artifact.content, "utf8");
  }
}

function resolveWorkspacePath(workspaceRoot: string, artifactPath: string): string {
  const root = resolve(workspaceRoot);
  const path = resolve(root, artifactPath);
  const relativePath = relative(root, path);
  if (relativePath.startsWith("..") || isAbsolute(relativePath)) {
    throw new Error(
      `Generated artifact path '${artifactPath}' must stay inside workspace '${workspaceRoot}'.`
    );
  }

  return path;
}

function failedDockerExecution(
  input: {
    readonly request: GeneratedNodeBuildLoopRequest;
  },
  message: string
): GeneratedNodeTestExecution {
  return {
    status: "failed",
    logs: [message],
    resultArtifacts: [],
    schemaValid: false,
    securityValid: false,
    replayValid: false,
    dependencyPolicyValid: false,
    findings: [
      {
        id: `finding.${input.request.nodeId}.docker`,
        severity: "error",
        target: { kind: "node", id: input.request.nodeId },
        message,
        issues: []
      }
    ],
    failureMessage: message
  };
}

function createFixturePayload(request: GeneratedNodeBuildLoopRequest): {
  readonly inputs: Record<string, unknown>;
} {
  return {
    inputs: Object.fromEntries(
      Object.keys(request.inputSchema)
        .sort()
        .map((port) => [port, { fixture: true }])
    )
  };
}

async function readDockerOutput(
  workspaceRoot: string,
  outputPath: string
): Promise<
  | { readonly ok: true; readonly output: Record<string, unknown>; readonly raw: string }
  | { readonly ok: false; readonly error: string; readonly raw: string }
> {
  try {
    return validateDockerOutput(
      await readFile(resolveWorkspacePath(workspaceRoot, outputPath), "utf8")
    );
  } catch (error) {
    const message =
      error instanceof Error ? error.message : `Unable to read Docker output '${outputPath}'.`;
    return {
      ok: false,
      error: message,
      raw: JSON.stringify({ error: message }, null, 2)
    };
  }
}

function validateDockerOutput(
  output: unknown
):
  | { readonly ok: true; readonly output: Record<string, unknown>; readonly raw: string }
  | { readonly ok: false; readonly error: string; readonly raw: string } {
  try {
    const parsed = typeof output === "string" ? JSON.parse(output) : output;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {
        ok: false,
        error: "Docker generated-node eval output must be a JSON object.",
        raw: typeof output === "string" ? output : JSON.stringify(output, null, 2)
      };
    }

    return {
      ok: true,
      output: parsed as Record<string, unknown>,
      raw: JSON.stringify(parsed, null, 2)
    };
  } catch (error) {
    const message =
      error instanceof Error
        ? error.message
        : "Docker generated-node eval output was invalid JSON.";
    return {
      ok: false,
      error: message,
      raw: typeof output === "string" ? output : JSON.stringify({ error: message }, null, 2)
    };
  }
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

function createModelInvocationRecord(
  input: GeneratedNodeRoleRunInput,
  model: {
    readonly provider: string;
    readonly model: string;
    readonly outputArtifact: string;
  }
): NonNullable<GeneratedNodeRoleRunResult["modelInvocations"]>[number] {
  return {
    id: `model.${input.request.job.id}.${input.role}.${input.iteration}`,
    role: input.role,
    inputSummary: input.inputSummary,
    outputArtifact: model.outputArtifact,
    provider: model.provider,
    model: model.model,
    determinismExpectation: "bounded",
    retryBudget: {
      maxAttempts: input.request.job.retry.maxAttempts,
      maxCostUsd: input.request.maxModelCostUsd
    },
    correlationId: input.request.job.correlationId,
    createdAt: input.request.generatedAt ?? new Date().toISOString()
  };
}

function throwIfAborted(request: GeneratedNodeBuildLoopRequest): void {
  if (request.signal?.aborted) {
    const reason = request.signal.reason;
    throw reason instanceof Error ? reason : new Error("Generated-node build was cancelled.");
  }
}
