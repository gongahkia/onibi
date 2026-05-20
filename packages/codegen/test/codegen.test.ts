import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AgentSdkCodeGenerator,
  AgentSdkGeneratedNodeRoleRunner,
  DockerGeneratedNodeTestExecutor,
  GeneratedNodeBuildLoop,
  LocalCodegenArtifactStore,
  assertSafeArtifactPath,
  createDependencyManifestArtifact,
  createArtifactManifest,
  createCodegenMetadata,
  createGeneratedArtifact,
  decideReplay,
  assertDependencyManifestPolicy
} from "../src/index.js";
import type {
  AgentQueryRunner,
  AgentRoleQueryRunner,
  CodeGenerator,
  CodegenGenerationRequest,
  DockerGeneratedNodeCommand,
  DockerGeneratedNodeCommandRunner,
  GeneratedNodeBuildLoopRequest,
  GeneratedNodeRoleRunner,
  GeneratedNodeTestExecutor
} from "../src/index.js";

describe("codegen artifact contracts", () => {
  it("creates artifacts with stable checksums", () => {
    const artifact = createGeneratedArtifact({
      path: "generated/workflow.ts",
      content: "export const workflow = true;\n",
      contentType: "text/typescript"
    });

    expect(artifact.checksum).toMatch(/^sha256:[a-f0-9]{64}$/);
  });

  it("rejects artifact paths outside the workspace", () => {
    expect(() => assertSafeArtifactPath("../secrets.txt")).toThrow("must be relative");
    expect(() => assertSafeArtifactPath("/tmp/output.txt")).toThrow("must be relative");
  });

  it("sorts manifest artifacts for stable diffs", () => {
    const second = createGeneratedArtifact({
      path: "b.ts",
      content: "b",
      contentType: "text/typescript"
    });
    const first = createGeneratedArtifact({
      path: "a.ts",
      content: "a",
      contentType: "text/typescript"
    });

    const manifest = createArtifactManifest({
      workflowId: "workflow.static-content",
      generatedAt: "2026-05-18T00:00:00.000Z",
      artifacts: [second, first]
    });

    expect(manifest.artifacts.map((artifact) => artifact.path)).toEqual(["a.ts", "b.ts"]);
  });

  it("decides replay behavior from drift and policy", () => {
    const previous = createArtifactManifest({
      workflowId: "workflow.static-content",
      generatedAt: "2026-05-18T00:00:00.000Z",
      artifacts: [
        createGeneratedArtifact({
          path: "a.ts",
          content: "a",
          contentType: "text/typescript"
        })
      ]
    });
    const next = createArtifactManifest({
      workflowId: "workflow.static-content",
      generatedAt: "2026-05-18T00:00:00.000Z",
      artifacts: [
        createGeneratedArtifact({
          path: "a.ts",
          content: "changed",
          contentType: "text/typescript"
        })
      ]
    });

    expect(
      decideReplay(previous, previous, { mode: "reuse-if-unchanged", seed: "test" }).action
    ).toBe("reuse");
    expect(decideReplay(previous, next, { mode: "fail-on-drift", seed: "test" }).action).toBe(
      "fail"
    );
  });

  it("creates workflow-compatible codegen metadata", () => {
    const artifact = createGeneratedArtifact({
      path: "generated/scrape-status-page.ts",
      content: "export const scrape = true;\n",
      contentType: "text/typescript"
    });
    const dependencyManifest = createGeneratedArtifact({
      path: "generated/package-manifest.json",
      content: JSON.stringify({ packageManager: "none", dependencies: [] }),
      contentType: "application/json"
    });

    expect(
      createCodegenMetadata({
        generator: "kelpclaw.codegen.typescript",
        generatedAt: "2026-05-18T00:00:00.000Z",
        sourcePrompt: "Scrape the page.",
        plannerRationale: "No deterministic registry skill matched the requested scraper.",
        artifact,
        dependencyManifest: {
          path: dependencyManifest.path,
          checksum: dependencyManifest.checksum,
          packageManager: "none",
          dependencies: [],
          devDependencies: [],
          installCommand: []
        },
        sandbox: {
          network: "none",
          allowedHosts: [],
          mounts: [],
          resources: {
            cpu: "1",
            memoryMb: 512
          }
        },
        replay: {
          mode: "reuse-if-unchanged",
          seed: "fixture"
        }
      })
    ).toEqual({
      originalPrompt: "Scrape the page.",
      latestPrompt: "Scrape the page.",
      plannerRationale: "No deterministic registry skill matched the requested scraper.",
      provenance: {
        generator: "kelpclaw.codegen.typescript",
        generatedAt: "2026-05-18T00:00:00.000Z",
        sourcePrompt: "Scrape the page.",
        artifactPath: "generated/scrape-status-page.ts",
        artifactChecksum: artifact.checksum
      },
      artifacts: [
        {
          path: "generated/package-manifest.json",
          checksum: dependencyManifest.checksum,
          contentType: "application/json"
        },
        {
          path: "generated/scrape-status-page.ts",
          checksum: artifact.checksum,
          contentType: "text/typescript"
        }
      ],
      dependencyManifest: {
        path: dependencyManifest.path,
        checksum: dependencyManifest.checksum,
        packageManager: "none",
        dependencies: [],
        devDependencies: [],
        installCommand: []
      },
      sandbox: {
        network: "none",
        allowedHosts: [],
        mounts: [],
        resources: {
          cpu: "1",
          memoryMb: 512
        }
      },
      review: {
        status: "draft"
      },
      replay: {
        mode: "reuse-if-unchanged",
        seed: "fixture"
      },
      llmBacked: false
    });
  });

  it("stores generated artifacts by content hash and materializes them", async () => {
    const storeRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-store-"));
    const targetRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-target-"));
    const store = new LocalCodegenArtifactStore(storeRoot);
    const artifact = createGeneratedArtifact({
      path: "generated/workflow.ts",
      content: "export const workflow = true;\n",
      contentType: "text/typescript"
    });

    const stored = await store.putArtifact(artifact);
    await store.putManifest(
      createArtifactManifest({
        workflowId: "workflow.static-content",
        generatedAt: "2026-05-18T00:00:00.000Z",
        artifacts: [artifact]
      })
    );
    const materialized = await store.materializeArtifacts([stored.ref], targetRoot);

    expect(stored.objectPath).toContain(artifact.checksum.replace("sha256:", ""));
    await expect(store.verifyArtifact(stored.ref)).resolves.toBe(true);
    expect(materialized).toEqual([join(targetRoot, "generated/workflow.ts")]);
    await expect(readFile(materialized[0]!, "utf8")).resolves.toBe(artifact.content);
  });

  it("enforces pinned generated dependency manifests", () => {
    expect(() =>
      assertDependencyManifestPolicy({
        packageManager: "npm",
        dependencies: ["left-pad"],
        devDependencies: [],
        installCommand: ["npm", "install"]
      })
    ).toThrow("must be pinned");

    const manifest = createDependencyManifestArtifact({
      packageManager: "npm",
      dependencies: ["left-pad@1.3.0"],
      installCommand: ["npm", "install", "--offline"]
    });
    expect(manifest.content).toContain("left-pad@1.3.0");
  });

  it("uses the Agent SDK runner and bounded repair for generated code", async () => {
    let calls = 0;
    const runner: AgentQueryRunner = async function* () {
      calls += 1;
      yield {
        type: "result",
        structured_output:
          calls === 1
            ? {
                sourceCode: "export {};",
                packageManager: "npm",
                dependencies: ["left-pad"],
                devDependencies: [],
                installCommand: ["npm", "install"]
              }
            : {
                sourceCode:
                  'import { writeFileSync } from "node:fs";\nwriteFileSync(process.env.NANOCLAW_NODE_OUTPUT!, JSON.stringify({ artifact: { ok: true } }));',
                packageManager: "none",
                dependencies: [],
                devDependencies: [],
                installCommand: []
              }
      };
    };
    const generator = new AgentSdkCodeGenerator({
      apiKey: "test-key",
      queryRunner: runner,
      maxRepairAttempts: 1
    });

    const result = await generator.generate(codegenRequestFixture());

    expect(calls).toBe(2);
    expect(result.sourceArtifact.path).toBe("generated/scrape-status-page.ts");
    expect(result.dependencyManifest.packageManager).toBe("none");
    expect(result.metadata.provenance.generator).toBe("anthropic.claude-agent-sdk");
  });

  it("fails clearly when live Agent SDK credentials are missing", async () => {
    const generator = new AgentSdkCodeGenerator({
      apiKey: ""
    });

    await expect(generator.generate(codegenRequestFixture())).rejects.toThrow(
      "ANTHROPIC_API_KEY is required"
    );
  });

  it("creates design, source, test, and eval agent artifacts through the build loop", async () => {
    const loop = new GeneratedNodeBuildLoop();
    const result = await loop.build(buildLoopRequestFixture());

    expect(result.designSpecArtifact.path).toBe("generated/scrape-status-page.design.json");
    expect(result.testArtifacts[0]?.path).toBe("generated/scrape-status-page.contract.test.ts");
    expect(result.generation.metadata.provenance.generator).toBe(
      "kelpclaw.codegen.deterministic-build-loop"
    );
    expect(result.agentRuns.map((run) => run.role)).toEqual([
      "workflow-architect",
      "coder",
      "tester",
      "runner",
      "evaluator"
    ]);
  });

  it("runs role-specific Agent SDK agents through the generated-node loop", async () => {
    const rolePrompts: string[] = [];
    const roleRunner: AgentRoleQueryRunner = async function* (prompt, options) {
      rolePrompts.push(`${options.model ?? "default"}:${prompt.split("\n")[0] ?? ""}`);
      yield {
        type: "result",
        total_cost_usd: 0.05,
        structured_output: {
          summary: `completed ${prompt.match(/You are the ([^ ]+) agent/u)?.[1] ?? "role"}`,
          status: "succeeded",
          outputArtifactRefs: []
        }
      };
    };
    const roles = [
      "workflow-architect",
      "coder",
      "tester",
      "runner",
      "fixer",
      "evaluator"
    ] as const;
    const loop = new GeneratedNodeBuildLoop({
      roleRunners: Object.fromEntries(
        roles.map((role) => [
          role,
          new AgentSdkGeneratedNodeRoleRunner({
            role,
            apiKey: "test-key",
            model: `claude-${role}`,
            queryRunner: roleRunner
          })
        ])
      ),
      testExecutor: failOnceThenPassExecutor("schema mismatch")
    });

    const result = await loop.build(buildLoopRequestFixture());

    expect(result.status).toBe("passed");
    expect(result.agentRuns.map((run) => run.role)).toContain("fixer");
    expect(result.agentRuns.every((run) => run.modelProvider === "anthropic")).toBe(true);
    expect(
      result.agentRuns.flatMap((run) => run.modelInvocations ?? []).map((record) => record.provider)
    ).toEqual(rolePrompts.map(() => "anthropic"));
    expect(rolePrompts.some((prompt) => prompt.startsWith("claude-fixer:"))).toBe(true);
  });

  it("runs generated-node evals through Docker when requested", async () => {
    const workspaceRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-docker-"));
    let capturedCommand: DockerGeneratedNodeCommand | undefined;
    const runner: DockerGeneratedNodeCommandRunner = {
      async run(command) {
        capturedCommand = command;
        return {
          exitCode: 0,
          stdout: "node ok\n",
          stderr: "",
          output: {
            artifact: {
              ok: true
            }
          }
        };
      }
    };
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: new DockerGeneratedNodeTestExecutor({ commandRunner: runner })
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      workspaceRoot,
      runTestsInDocker: true,
      maxDockerRuntimeSeconds: 7
    });

    expect(result.status).toBe("passed");
    expect(capturedCommand?.args).toContain("--network");
    expect(capturedCommand?.args).toContain("none");
    expect(capturedCommand?.args).toContain("node:20-alpine");
    expect(capturedCommand?.timeoutMs).toBe(7000);
    await expect(
      readFile(join(workspaceRoot, "generated/scrape-status-page.docker-command.json"), "utf8")
    ).resolves.toContain('"network": "none"');
    await expect(
      readFile(join(workspaceRoot, "generated/scrape-status-page.docker-output.json"), "utf8")
    ).resolves.toContain('"artifact"');
  });

  it("fails Docker evals when the output payload does not match declared ports", async () => {
    const workspaceRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-docker-fail-"));
    const runner: DockerGeneratedNodeCommandRunner = {
      async run() {
        return {
          exitCode: 0,
          stdout: "",
          stderr: "",
          output: {
            wrongPort: true
          }
        };
      }
    };
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: new DockerGeneratedNodeTestExecutor({ commandRunner: runner })
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      workspaceRoot,
      runTestsInDocker: true,
      maxIterations: 1
    });

    expect(result.status).toBe("failed");
    expect(result.findings.map((finding) => finding.id)).toContain(
      "finding.scrape-status-page.docker-schema"
    );
    expect(result.unresolvedFailureArtifact?.content).toContain("declared output ports");
  });

  it("emits unresolved failure artifacts after max generated-node iterations", async () => {
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: alwaysFailingTestExecutor("schema mismatch")
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      maxIterations: 2
    });

    expect(result.status).toBe("failed");
    expect(result.fixHistory).toHaveLength(2);
    expect(result.unresolvedFailureArtifact?.path).toBe(
      "generated/scrape-status-page.unresolved-failure.json"
    );
    expect(result.agentRuns.map((run) => run.role)).toContain("fixer");
  });

  it("stops the generated-node loop when the model budget is exhausted", async () => {
    const costlyCoder: GeneratedNodeRoleRunner = {
      role: "coder",
      async run(input) {
        const generation = await input.generateCode(input.request);
        return {
          status: "succeeded",
          inputSummary: input.inputSummary,
          outputArtifactRefs: [
            {
              path: generation.sourceArtifact.path,
              checksum: generation.sourceArtifact.checksum,
              contentType: generation.sourceArtifact.contentType
            }
          ],
          generation,
          modelCostUsd: 5
        };
      }
    };
    const loop = new GeneratedNodeBuildLoop({
      roleRunners: { coder: costlyCoder },
      testExecutor: alwaysFailingTestExecutor("needs repair")
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      maxIterations: 3,
      maxModelCostUsd: 1
    });

    expect(result.status).toBe("failed");
    expect(result.fixHistory[0]).toContain("needs repair");
    expect(result.unresolvedFailureArtifact?.content).toContain("model budget");
  });

  it("honors cancellation signals during generated-node builds", async () => {
    const controller = new AbortController();
    controller.abort(new Error("cancelled by test"));
    const loop = new GeneratedNodeBuildLoop();

    await expect(
      loop.build({
        ...buildLoopRequestFixture(),
        signal: controller.signal
      })
    ).rejects.toThrow("cancelled by test");
  });

  it("rejects generated files that escape the scoped workspace", async () => {
    const workspaceRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-escape-"));
    const dependencyManifestArtifact = createDependencyManifestArtifact({ packageManager: "none" });
    const safeArtifact = createGeneratedArtifact({
      path: "generated/safe.ts",
      content: "export {};",
      contentType: "text/typescript"
    });
    const generator: CodeGenerator = {
      async generate(request) {
        return {
          sourceArtifact: {
            ...safeArtifact,
            path: "../escape.ts"
          },
          dependencyManifestArtifact,
          dependencyManifest: {
            path: dependencyManifestArtifact.path,
            checksum: dependencyManifestArtifact.checksum,
            packageManager: "none",
            dependencies: [],
            devDependencies: [],
            installCommand: []
          },
          metadata: createCodegenMetadata({
            generator: "test.malicious",
            generatedAt: request.generatedAt ?? "2026-05-18T00:00:00.000Z",
            sourcePrompt: request.prompt,
            plannerRationale: request.plannerRationale,
            artifact: safeArtifact,
            dependencyManifest: {
              path: dependencyManifestArtifact.path,
              checksum: dependencyManifestArtifact.checksum,
              packageManager: "none",
              dependencies: [],
              devDependencies: [],
              installCommand: []
            },
            sandbox: request.sandbox,
            replay: {
              mode: "reuse-if-unchanged",
              seed: "test"
            }
          })
        };
      }
    };
    const loop = new GeneratedNodeBuildLoop({ codeGenerator: generator });

    await expect(
      loop.build({
        ...buildLoopRequestFixture(),
        workspaceRoot
      })
    ).rejects.toThrow("must stay inside workspace");
  });
});

function alwaysFailingTestExecutor(message: string): GeneratedNodeTestExecutor {
  return {
    async execute(input) {
      return {
        status: "failed",
        logs: [message],
        resultArtifacts: [],
        schemaValid: false,
        securityValid: true,
        replayValid: true,
        dependencyPolicyValid: true,
        findings: [
          {
            id: `finding.${input.request.nodeId}.test`,
            severity: "error",
            target: { kind: "node", id: input.request.nodeId },
            message,
            issues: []
          }
        ],
        failureMessage: message
      };
    }
  };
}

function failOnceThenPassExecutor(message: string): GeneratedNodeTestExecutor {
  let calls = 0;
  return {
    async execute(input) {
      calls += 1;
      if (calls === 1) {
        return alwaysFailingTestExecutor(message).execute(input);
      }

      return {
        status: "passed",
        logs: ["passed after repair"],
        resultArtifacts: [
          createGeneratedArtifact({
            path: `generated/${input.request.nodeId}.repaired-output.json`,
            content: JSON.stringify({ artifact: { ok: true } }, null, 2),
            contentType: "application/json"
          })
        ],
        schemaValid: true,
        securityValid: true,
        replayValid: true,
        dependencyPolicyValid: true,
        findings: []
      };
    }
  };
}

function buildLoopRequestFixture(): GeneratedNodeBuildLoopRequest {
  return {
    ...codegenRequestFixture(),
    job: {
      id: "job.build.codegen-node.test",
      type: "build.codegen-node",
      status: "running",
      workflowId: "workflow.scheduled-scraping",
      nodeId: "scrape-status-page",
      correlationId: "corr.codegen-test",
      createdAt: "2026-05-18T00:00:00.000Z",
      updatedAt: "2026-05-18T00:00:00.000Z",
      startedAt: "2026-05-18T00:00:00.000Z",
      retry: {
        attempt: 1,
        maxAttempts: 1,
        retryable: false
      },
      events: []
    },
    maxIterations: 3,
    maxWallClockSeconds: 600,
    maxModelCostUsd: 2,
    runTestsInDocker: false
  };
}

function codegenRequestFixture(): CodegenGenerationRequest {
  return {
    workflowId: "workflow.scheduled-scraping",
    nodeId: "scrape-status-page",
    prompt: "Scrape a public status page.",
    plannerRationale: "No deterministic registry skill matched the requested scraper.",
    inputSchema: {
      tick: { type: "object", additionalProperties: true }
    },
    outputSchema: {
      artifact: { type: "object", additionalProperties: true }
    },
    runtime: {
      image: "node:20-alpine",
      command: ["node", "/workspace/generated/scrape-status-page.js"],
      timeoutSeconds: 300,
      retry: {
        maxAttempts: 1,
        backoffSeconds: 0
      },
      environment: {},
      resources: {
        cpu: "1",
        memoryMb: 512
      }
    },
    sandbox: {
      network: "none",
      allowedHosts: [],
      mounts: [],
      resources: {
        cpu: "1",
        memoryMb: 512
      }
    },
    generatedAt: "2026-05-18T00:00:00.000Z"
  };
}
