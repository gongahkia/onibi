import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";
import {
  AgentSdkCodeGenerator,
  AgentSdkGeneratedNodeRoleRunner,
  DockerGeneratedNodeTestExecutor,
  GeneratedNodeBuildLoop,
  LocalCodegenArtifactStore,
  OpenAiCodeGenerator,
  OpenAiGeneratedNodeRoleRunner,
  OpenWeightCodeGenerator,
  OpenWeightGeneratedNodeRoleRunner,
  assertSafeArtifactPath,
  buildTbom,
  createOpenWeightChatCompletionsRunner,
  createDependencyManifestArtifact,
  createCrossAgentReplayRuns,
  createArtifactManifest,
  createCodegenMetadata,
  createGeneratedArtifact,
  createGeneratedModuleSignature,
  createAzureOpenAiResponsesRunner,
  decideReplay,
  generatedModuleSignaturesMatch,
  assertDependencyManifestPolicy,
  resolveAzureOpenAiResponsesConfig,
  synthesizeWorkflowFromTrajectory,
  trajectoryReplayShape,
  crossAgentReplaySkillMdFixture
} from "../src/index.js";
import { scheduledScrapingWorkflowFixture } from "@kelpclaw/workflow-spec";
import type {
  AgentQueryRunner,
  AgentRoleQueryRunner,
  CodeGenerator,
  CodegenGenerationRequest,
  DockerGeneratedNodeCommand,
  DockerGeneratedNodeCommandRunner,
  GeneratedNodeBuildLoopRequest,
  GeneratedNodeRoleRunner,
  GeneratedNodeTestExecutor,
  OpenWeightChatRunner,
  OpenAiResponsesRunner
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

  it("computes generated module signatures from reusable node contracts", () => {
    const node = scheduledScrapingWorkflowFixture.nodes.find(
      (candidate) => candidate.id === "scrape-status-page"
    );
    if (!node?.codegen) {
      throw new Error("Scheduled scraping fixture is missing a codegen node.");
    }

    const signature = createGeneratedModuleSignature(node);
    const dependencyDrift = createGeneratedModuleSignature({
      ...node,
      codegen: {
        ...node.codegen,
        dependencyManifest: {
          ...node.codegen.dependencyManifest,
          dependencies: ["undici@6.0.0"]
        }
      }
    });

    expect(signature.promptHash).toMatch(/^sha256:[a-f0-9]{64}$/);
    expect(generatedModuleSignaturesMatch(signature, createGeneratedModuleSignature(node))).toBe(
      true
    );
    expect(generatedModuleSignaturesMatch(signature, dependencyDrift)).toBe(false);
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

  it("uses OpenAI Responses structured output and bounded repair for generated code", async () => {
    let calls = 0;
    const runner: OpenAiResponsesRunner = async (request) => {
      calls += 1;
      expect(request.model).toBe("gpt-test-codegen");
      expect(request.text.format.type).toBe("json_schema");
      return {
        id: `resp_${calls}`,
        model: request.model,
        output_text: JSON.stringify(
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
        )
      };
    };
    const generator = new OpenAiCodeGenerator({
      apiKey: "test-key",
      model: "gpt-test-codegen",
      responsesRunner: runner,
      maxRepairAttempts: 1
    });

    const result = await generator.generate(codegenRequestFixture());

    expect(calls).toBe(2);
    expect(result.sourceArtifact.path).toBe("generated/scrape-status-page.ts");
    expect(result.dependencyManifest.packageManager).toBe("none");
    expect(result.metadata.provenance.generator).toBe("openai.responses");
    expect(result.metadata.llmBacked).toBe(true);
  });

  it("fails clearly when OpenAI live credentials are missing", async () => {
    const generator = new OpenAiCodeGenerator({
      apiKey: ""
    });

    await expect(generator.generate(codegenRequestFixture())).rejects.toThrow(
      "OPENAI_API_KEY or GPT5_MINI_API_KEY/GPT5_PRO_API_KEY is required"
    );
  });

  it("uses open-weight chat completions structured output and bounded repair", async () => {
    let calls = 0;
    const chatRunner: OpenWeightChatRunner = async (request) => {
      calls += 1;
      expect(request.model).toBe("qwen-test-codegen");
      expect(request.response_format.type).toBe("json_object");
      return {
        id: `chatcmpl_${calls}`,
        model: request.model,
        choices: [
          {
            message: {
              content:
                calls === 1
                  ? JSON.stringify({
                      sourceCode: "export {};",
                      packageManager: "npm",
                      dependencies: ["left-pad"],
                      devDependencies: [],
                      installCommand: ["npm", "install"]
                    })
                  : `Here is the JSON:\n\`\`\`json\n${JSON.stringify({
                      sourceCode:
                        'import { writeFileSync } from "node:fs";\nwriteFileSync(process.env.NANOCLAW_NODE_OUTPUT!, JSON.stringify({ artifact: { ok: true } }));',
                      packageManager: "none",
                      dependencies: [],
                      devDependencies: [],
                      installCommand: []
                    })}\n\`\`\``
            }
          }
        ]
      };
    };
    const generator = new OpenWeightCodeGenerator({
      baseUrl: "http://127.0.0.1:11434/v1",
      model: "qwen-test-codegen",
      chatRunner,
      maxRepairAttempts: 1
    });

    const result = await generator.generate(codegenRequestFixture());

    expect(calls).toBe(2);
    expect(result.sourceArtifact.path).toBe("generated/scrape-status-page.ts");
    expect(result.dependencyManifest.packageManager).toBe("none");
    expect(result.metadata.provenance.generator).toBe("openweight.chat-completions");
    expect(result.metadata.llmBacked).toBe(true);
  });

  it("fails clearly when open-weight base URL is missing", async () => {
    const generator = new OpenWeightCodeGenerator({
      baseUrl: ""
    });

    await expect(generator.generate(codegenRequestFixture())).rejects.toThrow(
      "KELPCLAW_OPENWEIGHT_BASE_URL is required"
    );
  });

  it("times out open-weight chat completions requests", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async (_url: unknown, init: unknown) =>
          await new Promise((_resolve, reject) => {
            const signal = (init as { readonly signal?: AbortSignal }).signal;
            signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
          })
      )
    );
    try {
      const runner = createOpenWeightChatCompletionsRunner({
        baseUrl: "http://127.0.0.1:11434/v1",
        timeoutMs: 1
      });
      await expect(
        runner({
          model: "qwen-timeout",
          messages: [{ role: "user", content: "return json" }],
          temperature: 0,
          stream: false,
          response_format: { type: "json_object" }
        })
      ).rejects.toThrow("timed out after 1ms");
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("resolves Azure OpenAI Responses config from GPT5 deployment env", () => {
    const previous = snapshotEnv([
      "GPT5_MINI_ENDPOINT",
      "GPT5_MINI_DEPLOYMENT",
      "GPT5_MINI_API_KEY",
      "GPT5_MINI_API_VERSION"
    ]);
    try {
      process.env.GPT5_MINI_ENDPOINT = "https://example.openai.azure.com/";
      process.env.GPT5_MINI_DEPLOYMENT = "gpt-mini";
      process.env.GPT5_MINI_API_KEY = "azure-key";
      process.env.GPT5_MINI_API_VERSION = "2025-04-01-preview";

      expect(resolveAzureOpenAiResponsesConfig()).toEqual({
        apiKey: "azure-key",
        endpoint: "https://example.openai.azure.com",
        deployment: "gpt-mini",
        apiVersion: "2025-04-01-preview"
      });
    } finally {
      restoreEnv(previous);
    }
  });

  it("prefers Azure OpenAI keys over a generic OpenAI override for Azure endpoints", () => {
    const previous = snapshotEnv([
      "GPT5_MINI_ENDPOINT",
      "GPT5_MINI_DEPLOYMENT",
      "GPT5_MINI_API_KEY",
      "GPT5_MINI_API_VERSION",
      "OPENAI_API_KEY"
    ]);
    try {
      process.env.GPT5_MINI_ENDPOINT = "https://example.openai.azure.com/";
      process.env.GPT5_MINI_DEPLOYMENT = "gpt-mini";
      process.env.GPT5_MINI_API_KEY = "azure-key";
      process.env.GPT5_MINI_API_VERSION = "2025-04-01-preview";
      process.env.OPENAI_API_KEY = "generic-openai-key";

      expect(resolveAzureOpenAiResponsesConfig(process.env.OPENAI_API_KEY)?.apiKey).toBe(
        "azure-key"
      );
    } finally {
      restoreEnv(previous);
    }
  });

  it("falls back to Azure Chat Completions when Responses is unavailable", async () => {
    const fetchMock = vi.fn(async (url: unknown, init: unknown) => {
      const requestUrl = String(url);
      if (requestUrl.includes("/responses")) {
        return new Response(JSON.stringify({ error: { code: "404" } }), {
          headers: { "content-type": "application/json" },
          status: 404
        });
      }

      const body = JSON.parse(String((init as { readonly body?: unknown }).body));
      expect(requestUrl).toBe(
        "https://example.openai.azure.com/openai/deployments/gpt-mini/chat/completions?api-version=2025-04-01-preview"
      );
      expect(body.messages).toEqual([
        { role: "system", content: "return structured json" },
        { role: "user", content: "generate code" }
      ]);
      expect(body.response_format).toEqual({
        type: "json_schema",
        json_schema: {
          name: "kelpclaw_generated_node",
          strict: true,
          schema: { type: "object", additionalProperties: false }
        }
      });
      return new Response(
        JSON.stringify({
          id: "chatcmpl.test",
          model: "gpt-mini",
          choices: [
            {
              message: {
                content:
                  '{"sourceCode":"export {};","packageManager":"none","dependencies":[],"devDependencies":[],"installCommand":[]}'
              }
            }
          ],
          usage: { total_tokens: 42 }
        }),
        { headers: { "content-type": "application/json" }, status: 200 }
      );
    });
    vi.stubGlobal("fetch", fetchMock);
    try {
      const runner = createAzureOpenAiResponsesRunner({
        apiKey: "azure-key",
        endpoint: "https://example.openai.azure.com",
        deployment: "gpt-mini",
        apiVersion: "2025-04-01-preview"
      });
      const result = await runner({
        model: "ignored",
        instructions: "return structured json",
        input: "generate code",
        store: false,
        tools: [],
        text: {
          format: {
            type: "json_schema",
            name: "kelpclaw_generated_node",
            strict: true,
            schema: { type: "object", additionalProperties: false }
          }
        }
      });

      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(result).toEqual({
        id: "chatcmpl.test",
        model: "gpt-mini",
        output_text:
          '{"sourceCode":"export {};","packageManager":"none","dependencies":[],"devDependencies":[],"installCommand":[]}',
        usage: { total_tokens: 42 }
      });
    } finally {
      vi.unstubAllGlobals();
    }
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

  it("triages small generated-node failures before applying a targeted repair", async () => {
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: failOnceThenPassExecutor("schema mismatch")
    });

    const result = await loop.build(buildLoopRequestFixture());

    expect(result.status).toBe("passed");
    expect(result.fixHistory[0]).toContain("targeted-patch/local-code");
    expect(result.agentRuns.filter((run) => run.role === "workflow-architect")).toHaveLength(1);
    expect(result.agentRuns.filter((run) => run.role === "coder")).toHaveLength(2);
  });

  it("reruns the architect only when fixer triage requires rearchitecture", async () => {
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: failOnceThenPassExecutor("workflow design mismatch")
    });

    const result = await loop.build(buildLoopRequestFixture());

    expect(result.status).toBe("passed");
    expect(result.fixHistory[0]).toContain("rearchitect/workflow-design");
    expect(result.agentRuns.filter((run) => run.role === "workflow-architect")).toHaveLength(2);
  });

  it("fails when the generated-node reimplementation loop exceeds its threshold", async () => {
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: alwaysFailingTestExecutor("workflow design mismatch")
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      maxIterations: 4,
      maxReimplementationAttempts: 1
    });

    expect(result.status).toBe("failed");
    expect(result.fixHistory.join("\n")).toContain("reimplementation threshold");
    expect(result.unresolvedFailureArtifact?.content).toContain(
      "exceeded 1 rearchitecture attempt"
    );
    expect(result.agentRuns.filter((run) => run.role === "workflow-architect")).toHaveLength(2);
  });

  it("stops generated-node repairs when fixer triage finds an external blocker", async () => {
    const loop = new GeneratedNodeBuildLoop({
      testExecutor: alwaysFailingTestExecutor("credential permission missing")
    });

    const result = await loop.build({
      ...buildLoopRequestFixture(),
      maxIterations: 3
    });

    expect(result.status).toBe("failed");
    expect(result.fixHistory).toHaveLength(1);
    expect(result.fixHistory[0]).toContain("give-up/external-blocker");
    expect(result.agentRuns.filter((run) => run.role === "coder")).toHaveLength(1);
  });

  it("runs role-specific Agent SDK agents through the generated-node loop", async () => {
    const rolePrompts: string[] = [];
    const roleRunner: AgentRoleQueryRunner = async function* (prompt, options) {
      rolePrompts.push(`${options.model ?? "default"}:${prompt.split("\n")[0] ?? ""}`);
      yield {
        type: "result",
        total_cost_usd: 0.05,
        duration_ms: 12,
        usage: {
          input_tokens: 10,
          output_tokens: 5
        },
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
    expect(result.agentRuns[0]?.modelInvocations?.[0]).toMatchObject({
      costUsd: 0.05,
      durationMs: 12,
      inputTokens: 10,
      outputTokens: 5,
      totalTokens: 15
    });
    expect(result.agentRuns[0]).toMatchObject({
      costUsd: 0.05,
      inputTokens: 10,
      outputTokens: 5,
      totalTokens: 15
    });
    expect(rolePrompts.some((prompt) => prompt.startsWith("claude-fixer:"))).toBe(true);
  });

  it("runs role-specific OpenAI agents through the generated-node loop", async () => {
    const rolePrompts: string[] = [];
    const responsesRunner: OpenAiResponsesRunner = async (request) => {
      if (request.text.format.name === "kelpclaw_generated_node") {
        return {
          id: "resp_codegen",
          model: request.model,
          output_text: JSON.stringify({
            sourceCode:
              'import { writeFileSync } from "node:fs";\nwriteFileSync(process.env.NANOCLAW_NODE_OUTPUT!, JSON.stringify({ artifact: { ok: true } }));',
            packageManager: "none",
            dependencies: [],
            devDependencies: [],
            installCommand: []
          })
        };
      }

      rolePrompts.push(`${request.model}:${request.input.split("\n")[0] ?? ""}`);
      return {
        id: `resp_${rolePrompts.length}`,
        model: request.model,
        output_text: JSON.stringify({
          summary: `completed ${request.input.match(/You are the ([^ ]+) agent/u)?.[1] ?? "role"}`,
          status: "succeeded",
          outputArtifactRefs: []
        }),
        total_cost_usd: 0.02,
        usage: {
          input_tokens: 7,
          input_tokens_details: {
            cached_tokens: 2
          },
          output_tokens: 3,
          output_tokens_details: {
            reasoning_tokens: 1
          },
          total_tokens: 10
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
      codeGenerator: new OpenAiCodeGenerator({
        apiKey: "test-key",
        model: "gpt-test-codegen",
        responsesRunner
      }),
      roleRunners: Object.fromEntries(
        roles.map((role) => [
          role,
          new OpenAiGeneratedNodeRoleRunner({
            role,
            apiKey: "test-key",
            model: `gpt-${role}`,
            responsesRunner
          })
        ])
      ),
      testExecutor: failOnceThenPassExecutor("schema mismatch")
    });

    const result = await loop.build(buildLoopRequestFixture());

    expect(result.status).toBe("passed");
    expect(result.generation.metadata.provenance.generator).toBe("openai.responses");
    expect(result.agentRuns.map((run) => run.role)).toContain("fixer");
    expect(result.agentRuns.every((run) => run.modelProvider === "openai")).toBe(true);
    expect(
      result.agentRuns.flatMap((run) => run.modelInvocations ?? []).map((record) => record.provider)
    ).toEqual(rolePrompts.map(() => "openai"));
    expect(result.agentRuns[0]?.modelInvocations?.[0]).toMatchObject({
      costUsd: 0.02,
      inputTokens: 7,
      outputTokens: 3,
      totalTokens: 10,
      cacheReadInputTokens: 2,
      modelUsage: {
        reasoningTokens: 1
      }
    });
    expect(result.agentRuns[0]).toMatchObject({
      costUsd: 0.02,
      inputTokens: 7,
      outputTokens: 3,
      totalTokens: 10,
      cacheReadInputTokens: 2
    });
    expect(rolePrompts.some((prompt) => prompt.startsWith("gpt-fixer:"))).toBe(true);
  });

  it("runs role-specific open-weight agents through the generated-node loop", async () => {
    const rolePrompts: string[] = [];
    const chatRunner: OpenWeightChatRunner = async (request) => {
      const userPrompt = request.messages.find((message) => message.role === "user")?.content ?? "";
      if (userPrompt.includes("Required JSON schema") && userPrompt.includes("sourceCode")) {
        return {
          id: "chatcmpl_codegen",
          model: request.model,
          choices: [
            {
              message: {
                content: JSON.stringify({
                  sourceCode:
                    'import { writeFileSync } from "node:fs";\nwriteFileSync(process.env.NANOCLAW_NODE_OUTPUT!, JSON.stringify({ artifact: { ok: true } }));',
                  packageManager: "none",
                  dependencies: [],
                  devDependencies: [],
                  installCommand: []
                })
              }
            }
          ]
        };
      }

      rolePrompts.push(`${request.model}:${userPrompt.split("\n")[0] ?? ""}`);
      return {
        id: `chatcmpl_${rolePrompts.length}`,
        model: request.model,
        choices: [
          {
            message: {
              content: `\`\`\`json\n${JSON.stringify({
                summary: `completed ${userPrompt.match(/You are the ([^ ]+) agent/u)?.[1] ?? "role"}`,
                status: "succeeded",
                outputArtifactRefs: []
              })}\n\`\`\``
            }
          }
        ],
        total_cost_usd: 0.01,
        usage: {
          prompt_tokens: 11,
          completion_tokens: 4,
          total_tokens: 15
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
      codeGenerator: new OpenWeightCodeGenerator({
        baseUrl: "http://127.0.0.1:11434/v1",
        model: "qwen-test-codegen",
        chatRunner
      }),
      roleRunners: Object.fromEntries(
        roles.map((role) => [
          role,
          new OpenWeightGeneratedNodeRoleRunner({
            role,
            baseUrl: "http://127.0.0.1:11434/v1",
            model: `qwen-${role}`,
            chatRunner
          })
        ])
      ),
      testExecutor: failOnceThenPassExecutor("schema mismatch")
    });

    const result = await loop.build(buildLoopRequestFixture());

    expect(result.status).toBe("passed");
    expect(result.generation.metadata.provenance.generator).toBe("openweight.chat-completions");
    expect(result.agentRuns.map((run) => run.role)).toContain("fixer");
    expect(result.agentRuns.every((run) => run.modelProvider === "openweight")).toBe(true);
    expect(
      result.agentRuns.flatMap((run) => run.modelInvocations ?? []).map((record) => record.provider)
    ).toEqual(rolePrompts.map(() => "openweight"));
    expect(result.agentRuns[0]?.modelInvocations?.[0]).toMatchObject({
      costUsd: 0.01,
      inputTokens: 11,
      outputTokens: 4,
      totalTokens: 15
    });
    expect(rolePrompts.some((prompt) => prompt.startsWith("qwen-fixer:"))).toBe(true);
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

  it("persists Docker timeout and stderr artifacts for failed evals", async () => {
    const workspaceRoot = await mkdtemp(join(tmpdir(), "kelpclaw-codegen-docker-timeout-"));
    const runner: DockerGeneratedNodeCommandRunner = {
      async run() {
        return {
          exitCode: 1,
          stdout: "",
          stderr: "runtime exceeded\n",
          timedOut: true,
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
      maxDockerRuntimeSeconds: 1,
      maxIterations: 1
    });

    expect(result.status).toBe("failed");
    expect(result.findings.map((finding) => finding.id)).toEqual(
      expect.arrayContaining([
        "finding.scrape-status-page.docker-timeout",
        "finding.scrape-status-page.docker-exit"
      ])
    );
    await expect(
      readFile(join(workspaceRoot, "generated/scrape-status-page.docker-stderr.log"), "utf8")
    ).resolves.toBe("runtime exceeded\n");
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
    expect(result.fixHistory).toHaveLength(0);
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

  it("synthesizes trajectory workflows and TBOMs without an LLM", () => {
    const run = {
      id: "agent-run.test",
      sourceAgent: "claude-code" as const,
      sessionId: "session.test",
      events: [
        {
          sourceAgent: "claude-code" as const,
          sessionId: "session.test",
          hookEvent: "PostToolUse",
          toolName: "Bash",
          toolUseId: "toolu.one",
          args: { command: "curl https://api.example.com/status", secret: "secret:api.token" },
          result: { provider: "anthropic", model: "claude-test" },
          status: "succeeded" as const,
          contentHash: `sha256:${"a".repeat(64)}`,
          prevEventHash: `sha256:${"b".repeat(64)}`,
          chainIndex: 0,
          classification: "Internal" as const,
          startedAt: "2026-05-23T00:00:00.000Z"
        },
        {
          sourceAgent: "claude-code" as const,
          sessionId: "session.test",
          hookEvent: "PostToolUse",
          toolName: "Bash",
          toolUseId: "toolu.two",
          args: { command: "cat output.json" },
          status: "succeeded" as const,
          contentHash: `sha256:${"c".repeat(64)}`,
          prevEventHash: `sha256:${"d".repeat(64)}`,
          chainIndex: 1,
          classification: "Internal" as const,
          startedAt: "2026-05-23T00:00:01.000Z"
        }
      ]
    };
    const workflow = synthesizeWorkflowFromTrajectory(run, {
      createdAt: "2026-05-23T00:00:00.000Z"
    });
    const tbom = buildTbom(workflow, run);

    expect(workflow.nodes.map((node) => node.kind)).toEqual(["trigger", "agent-step", "delivery"]);
    expect(workflow.nodes[1]?.config.callCount).toBe(2);
    expect(tbom.tools).toEqual([{ name: "Bash", calls: 2 }]);
    expect(tbom.externalDomains).toEqual(["api.example.com"]);
    expect(tbom.secretsConsumed).toEqual(["secret:api.token"]);
    expect(tbom.classifications).toEqual(["Internal"]);
  });

  it("keeps replay shape stable across Claude Code, Codex CLI, and Goose fixtures", () => {
    const runs = createCrossAgentReplayRuns();
    const shapes = runs.map(trajectoryReplayShape);
    const workflows = runs.map((run) =>
      synthesizeWorkflowFromTrajectory(run, { createdAt: "2026-05-23T00:00:00.000Z" })
    );

    expect(crossAgentReplaySkillMdFixture).toContain("KelpClaw Replay Smoke");
    expect(runs.map((run) => run.sourceAgent)).toEqual(["claude-code", "codex-cli", "goose"]);
    expect(new Set(shapes.map((shape) => JSON.stringify(shape))).size).toBe(1);
    expect(workflows.map((workflow) => workflow.nodes.map((node) => node.kind))).toEqual([
      ["trigger", "agent-step", "agent-step", "delivery"],
      ["trigger", "agent-step", "agent-step", "delivery"],
      ["trigger", "agent-step", "agent-step", "delivery"]
    ]);
    expect(
      workflows.map((workflow) =>
        workflow.nodes
          .filter((node) => node.kind === "agent-step")
          .map((node) => node.agentStep?.sourceAgent)
      )
    ).toEqual([
      ["claude-code", "claude-code"],
      ["codex-cli", "codex-cli"],
      ["goose", "goose"]
    ]);
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

function snapshotEnv(keys: readonly string[]): Record<string, string | undefined> {
  return Object.fromEntries(keys.map((key) => [key, process.env[key]]));
}

function restoreEnv(snapshot: Record<string, string | undefined>): void {
  for (const [key, value] of Object.entries(snapshot)) {
    if (value === undefined) {
      delete process.env[key];
    } else {
      process.env[key] = value;
    }
  }
}
