import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  AgentSdkCodeGenerator,
  LocalCodegenArtifactStore,
  assertSafeArtifactPath,
  createDependencyManifestArtifact,
  createArtifactManifest,
  createCodegenMetadata,
  createGeneratedArtifact,
  decideReplay,
  assertDependencyManifestPolicy
} from "../src/index.js";
import type { AgentQueryRunner, CodegenGenerationRequest } from "../src/index.js";

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
});

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
