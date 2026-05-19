import { execFileSync } from "node:child_process";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { createDefaultMockAdapters, createMockAdapter } from "@kelpclaw/adapters";
import {
  LocalCodegenArtifactStore,
  createArtifactManifest,
  createCodegenMetadata,
  createDependencyManifestArtifact,
  createGeneratedArtifact
} from "@kelpclaw/codegen";
import {
  WorkflowValidationError,
  approvedGmailReceiptsToSheetsWorkflowFixture,
  createApprovedWorkflowFixture,
  createWorkflowEdge,
  createWorkflowNode,
  createWorkflowRuntime,
  createWorkflowSpec,
  cyclicWorkflowFixture,
  gmailReceiptsToSheetsWorkflowFixture,
  objectSchema,
  timeSensitiveAlertDeliveryWorkflowFixture
} from "@kelpclaw/workflow-spec";
import {
  AdapterBackedNodeRunner,
  DockerNodeRunner,
  MockNodeRunner,
  ProductionNodeRunner,
  compileWorkflowDag,
  executeCompiledDag,
  hashWorkflowDag,
  replayCompletedRun
} from "../src/index.js";
import type { AdapterMetadata } from "@kelpclaw/adapters";
import type {
  CompiledDagNode,
  NodeRunContext,
  NodeRunner,
  NodeRunnerResult
} from "../src/index.js";

describe("nanoclaw dag runtime", () => {
  it("compiles only approved workflow revisions", () => {
    expect(() => compileWorkflowDag(gmailReceiptsToSheetsWorkflowFixture)).toThrow(
      WorkflowValidationError
    );

    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);

    expect(dag.order).toEqual([
      "manual-trigger",
      "read-gmail-receipts",
      "normalize-receipts",
      "append-sheet-rows",
      "deliver-results-email"
    ]);
    expect(dag.nodes.get("normalize-receipts")?.dependencies).toEqual(["read-gmail-receipts"]);
  });

  it("rejects cyclic workflow specs before execution", () => {
    expect(() => compileWorkflowDag(cyclicWorkflowFixture)).toThrow(WorkflowValidationError);
  });

  it("executes compiled dags through a mock runner in approved order", async () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new MockNodeRunner();
    const result = await executeCompiledDag(dag, runner, { secretResolver: mockSecretResolver });

    expect(result).toMatchObject({
      id: "execution.workflow.gmail-receipts-to-sheets.r1",
      workflowId: "workflow.gmail-receipts-to-sheets",
      revision: 1,
      status: "succeeded",
      deterministic: true
    });
    expect(runner.visitedNodeIds).toEqual(dag.order);
  });

  it("stops execution when a node fails", async () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new MockNodeRunner({ failingNodeIds: ["read-gmail-receipts"] });
    const result = await executeCompiledDag(dag, runner, { secretResolver: mockSecretResolver });

    expect(result.status).toBe("failed");
    expect(runner.visitedNodeIds).toEqual(["manual-trigger", "read-gmail-receipts"]);
    expect(result.nodeResults.map((nodeResult) => nodeResult.status)).toEqual([
      "succeeded",
      "failed",
      "skipped",
      "skipped",
      "skipped"
    ]);
  });

  it("executes adapter-backed Gmail to Sheets to email delivery with deterministic mocks", async () => {
    const adapters = createDefaultMockAdapters();
    const result = await executeCompiledDag(
      compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture),
      new AdapterBackedNodeRunner({ adapters }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("succeeded");
    expect(adapters.get("adapter.gmail")?.invocations).toHaveLength(1);
    expect(adapters.get("adapter.sheets")?.invocations).toHaveLength(1);
    expect(adapters.get("adapter.email")?.invocations).toHaveLength(1);
    expect(result.nodeResults.at(-1)?.output.delivery).toMatchObject({
      status: "succeeded",
      channels: ["email"]
    });
  });

  it("defaults delivery nodes to email when no secondary channel is declared", async () => {
    const workflow = approveForNanoClaw(
      createWorkflowSpec({
        id: "workflow.default-email-delivery",
        name: "Default Email Delivery",
        prompt: "Send final results.",
        createdAt: "2026-05-18T00:00:00.000Z",
        nodes: [
          createWorkflowNode({
            id: "emit-result",
            kind: "trigger",
            outputs: { request: objectSchema }
          }),
          createWorkflowNode({
            id: "deliver-result",
            kind: "delivery",
            inputs: { request: objectSchema },
            outputs: { delivery: objectSchema },
            secretRefs: {
              "email.delivery": "secret:email.smtp.default"
            },
            config: {
              channel: "email",
              allowedHosts: ["smtp"]
            }
          })
        ],
        edges: [
          createWorkflowEdge({
            sourceNodeId: "emit-result",
            sourcePort: "request",
            targetNodeId: "deliver-result",
            targetPort: "request"
          })
        ]
      })
    );
    const adapters = createDefaultMockAdapters();
    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      new AdapterBackedNodeRunner({ adapters }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("succeeded");
    expect(adapters.get("adapter.email")?.invocations).toHaveLength(1);
    expect(adapters.get("adapter.whatsapp")?.invocations).toHaveLength(0);
    expect(adapters.get("adapter.telegram")?.invocations).toHaveLength(0);
  });

  it("runs WhatsApp and Telegram only when the workflow opts into push channels", async () => {
    const adapters = createDefaultMockAdapters();
    const result = await executeCompiledDag(
      compileWorkflowDag(approveForNanoClaw(timeSensitiveAlertDeliveryWorkflowFixture)),
      new AdapterBackedNodeRunner({ adapters }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("succeeded");
    expect(adapters.get("adapter.whatsapp")?.invocations).toHaveLength(1);
    expect(adapters.get("adapter.telegram")?.invocations).toHaveLength(1);
    expect(result.nodeResults.at(-1)?.output.delivery).toMatchObject({
      channels: ["whatsapp", "telegram"]
    });
  });

  it("fails adapter-backed nodes with stable missing-secret validation errors", async () => {
    const workflow = approveForNanoClaw({
      ...gmailReceiptsToSheetsWorkflowFixture,
      nodes: gmailReceiptsToSheetsWorkflowFixture.nodes.map((node) =>
        node.id === "deliver-results-email"
          ? {
              ...node,
              secretRefs: {}
            }
          : node
      )
    });
    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      new AdapterBackedNodeRunner({ adapters: createDefaultMockAdapters() }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("failed");
    expect(result.nodeResults.at(-2)?.status).toBe("succeeded");
    expect(result.nodeResults.at(-1)?.error).toContain("WORKFLOW_ADAPTER_SECRET_MISSING");
  });

  it("enforces declared network policy before invoking adapters", async () => {
    const metadata: AdapterMetadata = {
      id: "adapter.email.networked",
      kind: "email",
      displayName: "Networked Email",
      version: "1.0.0",
      capabilities: ["email.results.send"],
      operations: [
        {
          name: "email.results.send",
          version: "1.0.0",
          description: "Send an email through a live network provider.",
          inputSchema: objectSchema,
          outputSchema: objectSchema
        }
      ],
      requiredSecrets: [],
      networkPolicy: {
        mode: "declared",
        allowedHosts: ["api.email.example.com"]
      },
      rateLimit: {
        maxRequests: 60,
        perSeconds: 60
      },
      retry: {
        maxAttempts: 1,
        backoffSeconds: 0,
        retryableErrorCodes: []
      },
      fixtures: [],
      live: false
    };
    const workflow = approveForNanoClaw(
      createWorkflowSpec({
        id: "workflow.network-policy",
        name: "Network Policy",
        prompt: "Send through a networked provider.",
        createdAt: "2026-05-18T00:00:00.000Z",
        nodes: [
          createWorkflowNode({
            id: "send-email",
            kind: "delivery",
            inputs: {},
            outputs: { delivery: objectSchema },
            adapterId: metadata.id,
            adapterIds: [metadata.id],
            adapterOperations: [
              {
                adapterId: metadata.id,
                operation: "email.results.send",
                operationVersion: "1.0.0"
              }
            ]
          })
        ],
        edges: []
      })
    );
    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      new AdapterBackedNodeRunner({
        adapters: new Map([[metadata.id, createMockAdapter(metadata)]])
      }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("failed");
    expect(result.nodeResults[0]?.error).toContain("WORKFLOW_ADAPTER_NETWORK_POLICY_INVALID");
  });

  it("uses stable node id tie-breaking independent of node insertion order", () => {
    const sourceA = createWorkflowNode({
      id: "a-source",
      kind: "trigger",
      outputs: { out: objectSchema }
    });
    const sourceB = createWorkflowNode({
      id: "b-source",
      kind: "trigger",
      outputs: { out: objectSchema }
    });
    const joinNode = createWorkflowNode({
      id: "join",
      kind: "transform",
      inputs: { a: objectSchema, b: objectSchema },
      outputs: { done: objectSchema }
    });
    const workflow = createWorkflowSpec({
      id: "workflow.stable-order",
      name: "Stable Order",
      prompt: "Join two independent sources.",
      createdAt: "2026-05-18T00:00:00.000Z",
      nodes: [joinNode, sourceB, sourceA],
      edges: [
        createWorkflowEdge({
          sourceNodeId: sourceB.id,
          sourcePort: "out",
          targetNodeId: joinNode.id,
          targetPort: "b"
        }),
        createWorkflowEdge({
          sourceNodeId: sourceA.id,
          sourcePort: "out",
          targetNodeId: joinNode.id,
          targetPort: "a"
        })
      ]
    });
    const approved = approveForNanoClaw(workflow, {
      nodeOrder: ["a-source", "b-source", "join"]
    });

    expect(compileWorkflowDag(approved).order).toEqual(["a-source", "b-source", "join"]);
  });

  it("uses production runner for live adapters and deterministic built-ins without MockNodeRunner", async () => {
    const adapters = createDefaultMockAdapters();
    const workflow = approveForNanoClaw(approvedGmailReceiptsToSheetsWorkflowFixture);
    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      new ProductionNodeRunner({
        adapters,
        hostWorkspace: "/tmp/kelpclaw"
      }),
      { secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("succeeded");
    expect(result.nodeResults.find((node) => node.nodeId === "manual-trigger")?.metadata).toEqual(
      expect.objectContaining({ deterministic: true })
    );
    expect(adapters.get("adapter.gmail")?.invocations).toHaveLength(1);
  });

  it("rejects approved workflows when the frozen DAG hash drifts", () => {
    const workflow = {
      ...approvedGmailReceiptsToSheetsWorkflowFixture,
      approval: {
        ...approvedGmailReceiptsToSheetsWorkflowFixture.approval!,
        frozenDagHash: `sha256:${"f".repeat(64)}`
      }
    };

    expect(() => compileWorkflowDag(workflow)).toThrow(WorkflowValidationError);
  });

  it("fails successful runner output that does not match declared schemas", async () => {
    const workflow = approveForNanoClaw(
      createWorkflowSpec({
        id: "workflow.invalid-output",
        name: "Invalid Output",
        prompt: "Return an invalid output shape.",
        createdAt: "2026-05-18T00:00:00.000Z",
        nodes: [
          createWorkflowNode({
            id: "manual-trigger",
            kind: "trigger",
            outputs: { request: objectSchema }
          })
        ],
        edges: []
      })
    );
    const dag = compileWorkflowDag(workflow);
    const runner = nodeRunner(() => ({
      status: "succeeded",
      output: { request: "not-an-object" }
    }));

    const result = await executeCompiledDag(dag, runner);

    expect(result.status).toBe("failed");
    expect(result.nodeResults[0]?.metadata?.validationDirection).toBe("output");
  });

  it("retries failed node attempts and records retry metadata", async () => {
    let calls = 0;
    const workflow = approveForNanoClaw(
      createWorkflowSpec({
        id: "workflow.retry",
        name: "Retry",
        prompt: "Retry once.",
        createdAt: "2026-05-18T00:00:00.000Z",
        nodes: [
          createWorkflowNode({
            id: "manual-trigger",
            kind: "trigger",
            outputs: { request: objectSchema },
            runtime: {
              retry: {
                maxAttempts: 2,
                backoffSeconds: 0
              }
            }
          })
        ],
        edges: []
      })
    );

    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      nodeRunner(() => {
        calls += 1;
        return calls === 1
          ? { status: "failed", output: {}, error: "transient" }
          : { status: "succeeded", output: { request: { ok: true } } };
      })
    );

    expect(result.status).toBe("succeeded");
    expect(result.nodeResults[0]?.attempts?.map((attempt) => attempt.status)).toEqual([
      "failed",
      "succeeded"
    ]);
    expect(result.nodeResults[0]?.metadata?.nonDeterministicRetry).toBe(true);
  });

  it("marks timed out node attempts without running downstream nodes", async () => {
    const workflow = approveForNanoClaw(
      createWorkflowSpec({
        id: "workflow.timeout",
        name: "Timeout",
        prompt: "Timeout a node.",
        createdAt: "2026-05-18T00:00:00.000Z",
        nodes: [
          createWorkflowNode({
            id: "manual-trigger",
            kind: "trigger",
            outputs: { request: objectSchema },
            runtime: {
              timeoutSeconds: 1,
              retry: {
                maxAttempts: 1,
                backoffSeconds: 0
              }
            }
          })
        ],
        edges: []
      })
    );

    const result = await executeCompiledDag(
      compileWorkflowDag(workflow),
      nodeRunner(
        (_node, context) =>
          new Promise<NodeRunnerResult>((_resolve, reject) => {
            context.signal?.addEventListener("abort", () => reject(context.signal?.reason), {
              once: true
            });
          })
      )
    );

    expect(result.status).toBe("failed");
    expect(result.nodeResults[0]?.attempts?.[0]?.status).toBe("timed_out");
  });

  it("marks cancelled node attempts", async () => {
    const controller = new AbortController();
    controller.abort(new Error("cancelled"));

    const result = await executeCompiledDag(
      compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture),
      nodeRunner((_node, context) =>
        Promise.reject(context.signal?.reason ?? new Error("cancelled"))
      ),
      { signal: controller.signal, secretResolver: mockSecretResolver }
    );

    expect(result.status).toBe("failed");
    expect(result.nodeResults[0]?.attempts?.[0]?.status).toBe("cancelled");
  });

  it("resolves declared secret refs at runtime without storing raw values in node payloads", async () => {
    const previous = process.env.KELP_TEST_SECRET;
    process.env.KELP_TEST_SECRET = "runtime-secret-value";
    const workflow = approveForNanoClaw({
      ...gmailReceiptsToSheetsWorkflowFixture,
      nodes: gmailReceiptsToSheetsWorkflowFixture.nodes.map((node) =>
        node.id === "manual-trigger"
          ? {
              ...node,
              secretRefs: {
                apiToken: "env:KELP_TEST_SECRET"
              }
            }
          : node
      )
    });

    try {
      const fallback = new MockNodeRunner();
      const result = await executeCompiledDag(
        compileWorkflowDag(workflow),
        nodeRunner((_node, context) => {
          if (context.inputPayload.nodeId === "manual-trigger") {
            expect(context.inputPayload.metadata).not.toHaveProperty("runtime-secret-value");
            expect(context.resolvedSecrets.KELPCLAW_SECRET_APITOKEN).toBe("runtime-secret-value");
          }

          return fallback.run(_node, context);
        }),
        { secretResolver: mockSecretResolver }
      );

      expect(result.status).toBe("succeeded");
    } finally {
      if (previous === undefined) {
        delete process.env.KELP_TEST_SECRET;
      } else {
        process.env.KELP_TEST_SECRET = previous;
      }
    }
  });

  it("constructs Docker-per-node commands without executing them", () => {
    const dag = compileWorkflowDag(approvedGmailReceiptsToSheetsWorkflowFixture);
    const runner = new DockerNodeRunner({ hostWorkspace: "/tmp/kelpclaw" });
    const command = runner.buildCommand(dag.nodes.get("manual-trigger")!);

    expect(command).toEqual([
      "docker",
      "run",
      "--rm",
      "--network",
      "none",
      "--volume",
      "/tmp/kelpclaw:/workspace",
      "--workdir",
      "/workspace",
      "node:20-alpine",
      "node",
      "/workspace/run-node.js"
    ]);
  });

  it("keeps codegen Docker networking disabled unless the sandbox declares it", () => {
    const { workflow } = createCodegenWorkflowFixture();
    const dag = compileWorkflowDag(approveForNanoClaw(workflow));
    const runner = new DockerNodeRunner({ hostWorkspace: "/tmp/kelpclaw" });
    const command = runner.buildCommand(dag.nodes.get("generated-node")!, {
      attempt: 1,
      resolvedSecrets: {},
      workspace: {
        runId: "run.codegen-network",
        nodeId: "generated-node",
        attempt: 1,
        nodeDir: "/tmp/kelpclaw/nodes/generated-node",
        attemptDir: "/tmp/kelpclaw/nodes/generated-node/attempt-1",
        inputPath: "/tmp/kelpclaw/nodes/generated-node/attempt-1/input.json",
        outputPath: "/tmp/kelpclaw/nodes/generated-node/attempt-1/output.json",
        stdoutPath: "/tmp/kelpclaw/nodes/generated-node/attempt-1/stdout.log",
        stderrPath: "/tmp/kelpclaw/nodes/generated-node/attempt-1/stderr.log",
        artifactsDir: "/tmp/kelpclaw/nodes/generated-node/attempt-1/artifacts",
        workflowSpecPath: "/tmp/kelpclaw/workflow.json"
      }
    });

    expect(command.slice(0, 5)).toEqual(["docker", "run", "--rm", "--network", "none"]);
  });

  it("materializes reviewed codegen artifacts before node execution", async () => {
    const store = new LocalCodegenArtifactStore(
      await mkdtemp(join(tmpdir(), "nanoclaw-codegen-store-"))
    );
    const { workflow, sourceArtifact, dependencyManifestArtifact } = createCodegenWorkflowFixture();
    await store.putManifest(
      createArtifactManifest({
        workflowId: workflow.id,
        generatedAt: workflow.nodes[0]!.codegen!.provenance.generatedAt,
        artifacts: [sourceArtifact, dependencyManifestArtifact]
      })
    );

    const result = await executeCompiledDag(
      compileWorkflowDag(approveForNanoClaw(workflow)),
      nodeRunner(async (_node, context) => {
        await expect(
          readFile(join(context.workspace.attemptDir, "run-node.js"), "utf8")
        ).resolves.toContain("persisted codegen source");

        return {
          status: "succeeded",
          output: { artifact: { ok: true } }
        };
      }),
      { codegenArtifactStore: store }
    );

    expect(result.status).toBe("succeeded");
    expect(result.nodeResults[0]?.metadata?.attempts).toBe(1);
  });

  it("fails codegen execution when persisted artifact hashes drift", async () => {
    const store = new LocalCodegenArtifactStore(
      await mkdtemp(join(tmpdir(), "nanoclaw-codegen-store-"))
    );
    const { workflow, dependencyManifestArtifact } = createCodegenWorkflowFixture({
      driftSourceChecksum: true
    });
    await store.putArtifact(dependencyManifestArtifact);

    const result = await executeCompiledDag(
      compileWorkflowDag(approveForNanoClaw(workflow)),
      nodeRunner(() => ({
        status: "succeeded",
        output: { artifact: { ok: true } }
      })),
      { codegenArtifactStore: store }
    );

    expect(result.status).toBe("failed");
    expect(result.nodeResults[0]?.error).toContain("hash drift");
  });
});

const dockerIt = dockerDaemonAvailable() ? it : it.skip;

describe("nanoclaw docker integration", () => {
  dockerIt(
    "runs two Dockerized nodes with isolated workspaces and replayable artifacts",
    async () => {
      const workflow = approveForNanoClaw(createDockerWorkflowFixture());
      const workspaceRoot = await mkdtemp(join(tmpdir(), "nanoclaw-docker-test-"));
      const result = await executeCompiledDag(
        compileWorkflowDag(workflow),
        new DockerNodeRunner({ hostWorkspace: workspaceRoot }),
        { workspaceRoot }
      );

      expect(result.status).toBe("succeeded");
      expect(result.nodeResults.map((nodeResult) => nodeResult.nodeId)).toEqual([
        "emit-request",
        "consume-request"
      ]);
      expect(result.nodeResults[0]?.workspacePath).not.toBe(result.nodeResults[1]?.workspacePath);
      expect(result.nodeResults[1]?.artifacts).toEqual(["result.txt"]);

      const secondInputPath = join(result.nodeResults[1]!.workspacePath!, "input.json");
      const secondInput = JSON.parse(await readFile(secondInputPath, "utf8"));
      expect(secondInput.inputs.request).toEqual({ value: 1 });

      const replayed = await replayCompletedRun(String(result.metadata?.manifestPath));
      expect(replayed.status).toBe("succeeded");
      expect(replayed.metadata?.replayed).toBe(true);
    },
    60_000
  );
});

function nodeRunner(
  run: (
    node: CompiledDagNode,
    context: NodeRunContext
  ) => NodeRunnerResult | Promise<NodeRunnerResult>
): NodeRunner {
  return {
    async run(node, context) {
      return run(node, context);
    }
  };
}

function approveForNanoClaw(
  workflow: ReturnType<typeof createWorkflowSpec>,
  override: Partial<NonNullable<ReturnType<typeof createWorkflowSpec>["approval"]>> = {}
) {
  return createApprovedWorkflowFixture(workflow, {
    frozenDagHash: hashWorkflowDag(workflow),
    ...override
  });
}

function createCodegenWorkflowFixture(options: { readonly driftSourceChecksum?: boolean } = {}) {
  const runtime = createWorkflowRuntime();
  const sourceArtifact = createGeneratedArtifact({
    path: "generated/generated-node.ts",
    content: [
      'import { writeFileSync } from "node:fs";',
      "// persisted codegen source",
      "writeFileSync(process.env.NANOCLAW_NODE_OUTPUT, JSON.stringify({ artifact: { ok: true } }));",
      ""
    ].join("\n"),
    contentType: "text/typescript"
  });
  const dependencyManifestArtifact = createDependencyManifestArtifact({
    packageManager: "none"
  });
  const dependencyManifest = {
    path: dependencyManifestArtifact.path,
    checksum: dependencyManifestArtifact.checksum,
    packageManager: "none" as const,
    dependencies: [],
    devDependencies: [],
    installCommand: []
  };
  const driftChecksum = `sha256:${"e".repeat(64)}`;
  const metadata = createCodegenMetadata({
    generator: "kelpclaw.codegen.test",
    generatedAt: "2026-05-18T00:00:00.000Z",
    sourcePrompt: "Generate a deterministic node.",
    plannerRationale: "No registry skill matched.",
    artifact: sourceArtifact,
    dependencyManifest,
    sandbox: {
      network: "none",
      allowedHosts: [],
      mounts: [],
      resources: runtime.resources
    },
    replay: {
      mode: "reuse-if-unchanged",
      seed: "codegen-test"
    }
  });
  const sourceChecksum = options.driftSourceChecksum ? driftChecksum : sourceArtifact.checksum;
  const codegen = {
    ...metadata,
    provenance: {
      ...metadata.provenance,
      artifactChecksum: sourceChecksum
    },
    artifacts: metadata.artifacts.map((artifact) =>
      artifact.path === sourceArtifact.path ? { ...artifact, checksum: sourceChecksum } : artifact
    ),
    review: {
      status: "approved" as const,
      reviewedBy: "owner@example.com",
      reviewedAt: "2026-05-18T01:00:00.000Z"
    }
  };
  const generatedNode = createWorkflowNode({
    id: "generated-node",
    kind: "codegen",
    inputs: {},
    outputs: { artifact: objectSchema },
    runtime,
    codegen
  });
  const workflow = createWorkflowSpec({
    id: "workflow.codegen-runtime",
    name: "Codegen Runtime",
    prompt: "Execute reviewed generated code.",
    createdAt: "2026-05-18T00:00:00.000Z",
    nodes: [generatedNode],
    edges: []
  });

  return {
    workflow,
    sourceArtifact,
    dependencyManifestArtifact
  };
}

function createDockerWorkflowFixture() {
  const emitCommand = [
    "node",
    "-e",
    [
      'const fs = require("fs");',
      'console.log("emit-request");',
      "fs.writeFileSync(process.env.NANOCLAW_NODE_OUTPUT, JSON.stringify({ request: { value: 1 } }));"
    ].join(" ")
  ];
  const consumeCommand = [
    "node",
    "-e",
    [
      'const fs = require("fs");',
      'const input = JSON.parse(fs.readFileSync(process.env.NANOCLAW_NODE_INPUT, "utf8"));',
      'fs.writeFileSync(`${process.env.NANOCLAW_ARTIFACTS_DIR}/result.txt`, "artifact");',
      'console.log("consume-request");',
      "fs.writeFileSync(process.env.NANOCLAW_NODE_OUTPUT, JSON.stringify({ result: input.inputs.request }));"
    ].join(" ")
  ];
  const emitRequest = createWorkflowNode({
    id: "emit-request",
    kind: "trigger",
    outputs: { request: objectSchema },
    runtime: createWorkflowRuntime({ command: emitCommand })
  });
  const consumeRequest = createWorkflowNode({
    id: "consume-request",
    kind: "transform",
    inputs: { request: objectSchema },
    outputs: { result: objectSchema },
    runtime: createWorkflowRuntime({ command: consumeCommand })
  });

  return createWorkflowSpec({
    id: "workflow.docker-integration",
    name: "Docker Integration",
    prompt: "Run two Dockerized nodes.",
    createdAt: "2026-05-18T00:00:00.000Z",
    nodes: [emitRequest, consumeRequest],
    edges: [
      createWorkflowEdge({
        sourceNodeId: emitRequest.id,
        sourcePort: "request",
        targetNodeId: consumeRequest.id,
        targetPort: "request"
      })
    ]
  });
}

function dockerDaemonAvailable(): boolean {
  try {
    execFileSync("docker", ["info", "--format", "{{.ServerVersion}}"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const mockSecretResolver = {
  async resolve(secretRef: string): Promise<string> {
    if (secretRef.startsWith("env:")) {
      return process.env[secretRef.slice("env:".length)] ?? secretRef;
    }
    if (secretRef === "secret:google.oauth.default") {
      return JSON.stringify({ accessToken: "test-google-access-token" });
    }
    if (secretRef === "secret:email.smtp.default") {
      return JSON.stringify({ host: "smtp.test", from: "owner@example.com" });
    }
    if (secretRef === "secret:whatsapp.cloud.default") {
      return JSON.stringify({ accessToken: "whatsapp-token", phoneNumberId: "phone-1" });
    }
    if (secretRef === "secret:telegram.bot.default") {
      return JSON.stringify({ botToken: "telegram-token", chatId: "ops" });
    }

    return secretRef;
  }
};
