import {
  AgentSdkCodeGenerator,
  LocalCodegenArtifactStore,
  createArtifactManifest,
  createDependencyManifestArtifact,
  createGeneratedArtifact
} from "@kelpclaw/codegen";
import { chooseSkillOrCodegen } from "@kelpclaw/skill-registry";
import {
  createWorkflowSpecDiff,
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture,
  workflowIdFromPrompt
} from "@kelpclaw/workflow-spec";
import type {
  CodegenArtifactStore,
  CodegenGenerationRequest,
  CodegenGenerationResult,
  CodeGenerator
} from "@kelpclaw/codegen";
import type {
  WorkflowNode,
  WorkflowPlanRequest,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";

export interface WorkflowPlannerBackend {
  plan(request: WorkflowPlanRequest): Promise<WorkflowSpec>;
}

export interface RegistryPlannerBackendOptions {
  readonly codeGenerator: CodeGenerator;
  readonly artifactStore: CodegenArtifactStore;
}

export type PlannerBackendMode = "deterministic" | "live";
export type PlannerBackendProvider = "anthropic";

export interface LivePlannerBackendOptions extends Partial<RegistryPlannerBackendOptions> {
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
}

export function createLivePlannerBackend(
  options: LivePlannerBackendOptions = {}
): WorkflowPlannerBackend {
  return new RegistryPlannerBackend({
    codeGenerator:
      options.codeGenerator ??
      new AgentSdkCodeGenerator({
        apiKey: options.apiKey,
        model: options.model
      }),
    artifactStore: options.artifactStore ?? new LocalCodegenArtifactStore()
  });
}

export function createDeterministicPlannerBackend(
  options: Partial<RegistryPlannerBackendOptions> = {}
): WorkflowPlannerBackend {
  return new RegistryPlannerBackend({
    codeGenerator: options.codeGenerator ?? new DeterministicCodeGenerator(),
    artifactStore: options.artifactStore ?? new LocalCodegenArtifactStore()
  });
}

export function createPlannerBackendFromEnv(
  options: Partial<RegistryPlannerBackendOptions> = {}
): WorkflowPlannerBackend {
  const mode = plannerModeFromEnv();
  if (mode === "deterministic") {
    return createDeterministicPlannerBackend(options);
  }

  const provider = plannerProviderFromEnv();
  if (provider !== "anthropic") {
    throw new Error(`Unsupported planner provider '${provider}'.`);
  }

  return createLivePlannerBackend({
    ...options,
    apiKey: process.env.ANTHROPIC_API_KEY,
    model: process.env.KELPCLAW_PLANNER_MODEL
  });
}

export async function planWorkflowDraft(
  request: WorkflowPlanRequest,
  planner: WorkflowPlannerBackend = createLivePlannerBackend()
): Promise<WorkflowSpec> {
  return planner.plan(request);
}

function plannerModeFromEnv(): PlannerBackendMode {
  const mode = process.env.KELPCLAW_PLANNER_MODE ?? "live";
  if (mode === "deterministic" || mode === "live") {
    return mode;
  }

  throw new Error("KELPCLAW_PLANNER_MODE must be 'deterministic' or 'live'.");
}

function plannerProviderFromEnv(): PlannerBackendProvider {
  const provider = process.env.KELPCLAW_PLANNER_PROVIDER ?? "anthropic";
  if (provider === "anthropic") {
    return provider;
  }

  throw new Error("KELPCLAW_PLANNER_PROVIDER must be 'anthropic'.");
}

export function planMockWorkflowDraft(request: WorkflowPlanRequest): WorkflowSpec {
  const prompt = request.prompt.trim();
  return finalizeTemplateWorkflow({
    request,
    prompt,
    template: chooseTemplate(prompt),
    nodes: chooseTemplate(prompt).nodes
  });
}

class RegistryPlannerBackend implements WorkflowPlannerBackend {
  private readonly codeGenerator: CodeGenerator;
  private readonly artifactStore: CodegenArtifactStore;

  public constructor(options: RegistryPlannerBackendOptions) {
    this.codeGenerator = options.codeGenerator;
    this.artifactStore = options.artifactStore;
  }

  public async plan(request: WorkflowPlanRequest): Promise<WorkflowSpec> {
    const prompt = request.prompt.trim();
    const template = chooseTemplate(prompt);

    if (template.id !== scheduledScrapingWorkflowFixture.id) {
      return finalizeTemplateWorkflow({
        request,
        prompt,
        template,
        nodes: template.nodes.map((node) => annotateSkillPlanning(node, prompt))
      });
    }

    return this.planScrapingWorkflow(request, prompt);
  }

  private async planScrapingWorkflow(
    request: WorkflowPlanRequest,
    prompt: string
  ): Promise<WorkflowSpec> {
    const workflowId = request.currentWorkflow?.id ?? workflowIdFromPrompt(prompt);
    const selection = chooseSkillOrCodegen({
      nodeKind: "skill",
      capability: "public-status-scrape",
      prompt
    });
    const codegenNode = scheduledScrapingWorkflowFixture.nodes.find(
      (node) => node.id === "scrape-status-page"
    );
    if (!codegenNode) {
      throw new Error("Scheduled scraping template is missing its generated node.");
    }

    const nodes =
      selection.kind === "skill"
        ? scheduledScrapingWorkflowFixture.nodes.map((node) =>
            node.id === codegenNode.id
              ? {
                  ...node,
                  kind: "skill" as const,
                  label: selection.match.skill.name,
                  description: selection.match.skill.description,
                  inputs: selection.match.skill.inputSchema,
                  outputs: selection.match.skill.outputSchema,
                  runtime: selection.match.skill.runtimeTemplate,
                  skillId: selection.match.skill.id,
                  adapterId: selection.match.skill.adapterDependencies[0],
                  adapterIds: selection.match.skill.adapterDependencies,
                  adapterOperations: selection.match.skill.adapterOperations,
                  codegen: undefined,
                  config: {
                    ...node.config,
                    plannerRationale: selection.match.reasons.join(" ")
                  }
                }
              : node
          )
        : await this.withGeneratedScraperNode({
            workflowId,
            prompt,
            templateNodes: scheduledScrapingWorkflowFixture.nodes,
            codegenNode,
            plannerRationale: selection.reasons.join(" ")
          });

    return finalizeTemplateWorkflow({
      request,
      prompt,
      template: scheduledScrapingWorkflowFixture,
      nodes
    });
  }

  private async withGeneratedScraperNode(input: {
    readonly workflowId: string;
    readonly prompt: string;
    readonly templateNodes: WorkflowSpec["nodes"];
    readonly codegenNode: WorkflowNode;
    readonly plannerRationale: string;
  }): Promise<WorkflowSpec["nodes"]> {
    const generated = await this.codeGenerator.generate({
      workflowId: input.workflowId,
      nodeId: input.codegenNode.id,
      prompt: input.prompt,
      plannerRationale: input.plannerRationale,
      inputSchema: input.codegenNode.inputs,
      outputSchema: input.codegenNode.outputs,
      runtime: input.codegenNode.runtime,
      sandbox: {
        network: input.codegenNode.determinism.externalCalls.length > 0 ? "declared" : "none",
        allowedHosts: input.codegenNode.determinism.externalCalls.map((call) =>
          call.replace(/^https?:\/\//u, "")
        ),
        mounts: [],
        resources: input.codegenNode.runtime.resources
      }
    });
    await this.artifactStore.putManifest(
      createArtifactManifest({
        workflowId: input.workflowId,
        generatedAt: generated.metadata.provenance.generatedAt,
        artifacts: [generated.sourceArtifact, generated.dependencyManifestArtifact]
      })
    );

    return input.templateNodes.map((node) =>
      node.id === input.codegenNode.id
        ? {
            ...node,
            config: {
              ...node.config,
              artifactStatus: "draft",
              plannerRationale: input.plannerRationale
            },
            codegen: generated.metadata
          }
        : node
    );
  }
}

class DeterministicCodeGenerator implements CodeGenerator {
  public async generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult> {
    const factory = new AgentlessGeneratedNodeFactory();
    const sourceArtifact = factory.createSourceArtifact(request.nodeId);
    const dependencyManifestArtifact = factory.createDependencyManifestArtifact();
    const dependencyManifest = {
      path: dependencyManifestArtifact.path,
      checksum: dependencyManifestArtifact.checksum,
      packageManager: "none" as const,
      dependencies: [],
      devDependencies: [],
      installCommand: []
    };

    return {
      sourceArtifact,
      dependencyManifestArtifact,
      dependencyManifest,
      metadata: {
        originalPrompt: request.prompt,
        latestPrompt: request.prompt,
        plannerRationale: request.plannerRationale,
        provenance: {
          generator: "kelpclaw.codegen.deterministic-test",
          generatedAt: request.generatedAt ?? new Date().toISOString(),
          sourcePrompt: request.prompt,
          artifactPath: sourceArtifact.path,
          artifactChecksum: sourceArtifact.checksum
        },
        artifacts: [
          {
            path: sourceArtifact.path,
            checksum: sourceArtifact.checksum,
            contentType: sourceArtifact.contentType
          },
          {
            path: dependencyManifestArtifact.path,
            checksum: dependencyManifestArtifact.checksum,
            contentType: dependencyManifestArtifact.contentType
          }
        ].sort((left, right) => left.path.localeCompare(right.path)),
        dependencyManifest,
        sandbox: request.sandbox,
        review: {
          status: "draft"
        },
        replay: {
          mode: "reuse-if-unchanged",
          seed: `${request.workflowId}.${request.nodeId}`
        },
        llmBacked: false
      }
    };
  }
}

class AgentlessGeneratedNodeFactory {
  public createSourceArtifact(nodeId: string) {
    return createGeneratedArtifact({
      path: `generated/${nodeId}.ts`,
      content: [
        'import { dirname } from "node:path";',
        'import { mkdirSync, readFileSync, writeFileSync } from "node:fs";',
        "",
        'const inputPath = process.env.NANOCLAW_NODE_INPUT ?? "/workspace/input.json";',
        'const outputPath = process.env.NANOCLAW_NODE_OUTPUT ?? "/workspace/output.json";',
        'const payload = JSON.parse(readFileSync(inputPath, "utf8"));',
        "const page = { generated: true, inputs: payload.inputs };",
        "mkdirSync(dirname(outputPath), { recursive: true });",
        'writeFileSync(outputPath, JSON.stringify({ page }, null, 2), "utf8");',
        ""
      ].join("\n"),
      contentType: "text/typescript"
    });
  }

  public createDependencyManifestArtifact() {
    return createDependencyManifestArtifact({
      packageManager: "none"
    });
  }
}

export function repromptWorkflowNode(
  workflow: WorkflowSpec,
  request: WorkflowRepromptNodeRequest
): WorkflowRepromptNodeResponse["after"] {
  const before = workflow.nodes.find((node) => node.id === request.nodeId);
  if (!before) {
    throw new Error(`Workflow node '${request.nodeId}' was not found.`);
  }

  const now = new Date().toISOString();
  const nodePrompt = request.prompt.trim();
  const after = {
    ...before,
    label: labelFromPrompt(nodePrompt, before.label),
    description: nodePrompt || before.description,
    config: {
      ...before.config,
      nodePrompt,
      repromptedAt: now
    },
    ...(before.codegen
      ? {
          codegen: {
            ...before.codegen,
            latestPrompt: nodePrompt || before.codegen.latestPrompt,
            review: {
              status: "draft" as const,
              notes: "Reprompted node requires generated artifact review."
            }
          }
        }
      : {})
  };

  return after;
}

export function repromptWorkflow(
  workflow: WorkflowSpec,
  request: WorkflowRepromptNodeRequest
): {
  readonly workflow: WorkflowSpec;
  readonly before: WorkflowRepromptNodeResponse["before"];
  readonly after: WorkflowRepromptNodeResponse["after"];
  readonly diff: WorkflowRepromptNodeResponse["diff"];
} {
  const before = workflow.nodes.find((node) => node.id === request.nodeId);
  if (!before) {
    throw new Error(`Workflow node '${request.nodeId}' was not found.`);
  }

  const after = repromptWorkflowNode(workflow, request);
  const nextWorkflow: WorkflowSpec = {
    ...workflow,
    nodes: workflow.nodes.map((node) => (node.id === request.nodeId ? after : node)),
    approval: null,
    updatedAt: new Date().toISOString()
  };

  return {
    workflow: nextWorkflow,
    before,
    after,
    diff: createWorkflowSpecDiff(workflow, nextWorkflow)
  };
}

function finalizeTemplateWorkflow(input: {
  readonly request: WorkflowPlanRequest;
  readonly prompt: string;
  readonly template: WorkflowSpec;
  readonly nodes: WorkflowSpec["nodes"];
}): WorkflowSpec {
  const now = new Date().toISOString();
  const currentWorkflow = input.request.currentWorkflow;
  const preservedNodes = new Map(
    currentWorkflow?.nodes
      .filter((node) => input.request.preserveNodeIds?.includes(node.id))
      .map((node) => [node.id, node]) ?? []
  );
  const nodes = input.nodes.map((node) => preservedNodes.get(node.id) ?? node);
  const workflowId = currentWorkflow?.id ?? workflowIdFromPrompt(input.prompt);

  return {
    ...input.template,
    id: workflowId,
    name: titleFromPrompt(input.prompt),
    prompt: input.prompt,
    revision: currentWorkflow ? currentWorkflow.revision + 1 : 1,
    nodes,
    approval: null,
    createdAt: currentWorkflow?.createdAt ?? now,
    updatedAt: now
  };
}

function annotateSkillPlanning(node: WorkflowNode, prompt: string): WorkflowNode {
  if (node.kind !== "skill" && node.kind !== "delivery") {
    return node;
  }

  const selection = chooseSkillOrCodegen({
    skillId: node.skillId,
    nodeKind: node.kind,
    adapterDependencies: node.adapterIds ?? (node.adapterId ? [node.adapterId] : []),
    prompt
  });

  return {
    ...node,
    config: {
      ...node.config,
      plannerRationale:
        selection.kind === "skill" ? selection.match.reasons.join(" ") : selection.reasons.join(" ")
    }
  };
}

function chooseTemplate(prompt: string): WorkflowSpec {
  const normalizedPrompt = prompt.toLowerCase();
  if (
    normalizedPrompt.includes("alert") ||
    normalizedPrompt.includes("telegram") ||
    normalizedPrompt.includes("whatsapp") ||
    normalizedPrompt.includes("support")
  ) {
    return timeSensitiveAlertDeliveryWorkflowFixture;
  }

  if (
    normalizedPrompt.includes("scrape") ||
    normalizedPrompt.includes("status page") ||
    normalizedPrompt.includes("regex") ||
    normalizedPrompt.includes("api call") ||
    normalizedPrompt.includes("code") ||
    normalizedPrompt.includes("artifact")
  ) {
    return scheduledScrapingWorkflowFixture;
  }

  return gmailReceiptsToSheetsWorkflowFixture;
}

function titleFromPrompt(prompt: string): string {
  return prompt
    .split(/[^a-z0-9]+/iu)
    .filter(Boolean)
    .slice(0, 5)
    .map((token) => token.charAt(0).toUpperCase() + token.slice(1).toLowerCase())
    .join(" ");
}

function labelFromPrompt(prompt: string, fallback: string): string {
  const title = titleFromPrompt(prompt);
  return title || fallback;
}
