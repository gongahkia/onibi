import type { Options, SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { createCodegenMetadata, createGeneratedArtifact } from "./artifacts.js";
import {
  assertDependencyManifestPolicy,
  createDependencyManifestArtifact,
  dependencyManifestFromArtifact
} from "./dependency-policy.js";
import type { DependencyManifestInput } from "./dependency-policy.js";
import type { CodegenGenerationRequest, CodegenGenerationResult, CodeGenerator } from "./types.js";

export interface AgentSdkCodeGeneratorOptions {
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
  readonly maxRepairAttempts?: number | undefined;
  readonly queryRunner?: AgentQueryRunner | undefined;
}

export type AgentQueryRunner = (
  prompt: string,
  options: Options
) => AsyncIterable<Pick<SDKMessage, "type"> & Record<string, unknown>>;

interface AgentStructuredOutput {
  readonly sourceCode: string;
  readonly packageManager: DependencyManifestInput["packageManager"];
  readonly dependencies: readonly string[];
  readonly devDependencies: readonly string[];
  readonly installCommand: readonly string[];
}

export class AgentSdkCodeGenerator implements CodeGenerator {
  private readonly apiKey: string | undefined;
  private readonly model: string | undefined;
  private readonly maxRepairAttempts: number;
  private readonly queryRunner: AgentQueryRunner | undefined;

  public constructor(options: AgentSdkCodeGeneratorOptions = {}) {
    this.apiKey = options.apiKey ?? process.env.ANTHROPIC_API_KEY;
    this.model = options.model ?? process.env.KELPCLAW_PLANNER_MODEL;
    this.maxRepairAttempts = options.maxRepairAttempts ?? 1;
    this.queryRunner = options.queryRunner;
  }

  public async generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult> {
    const runner = await this.getQueryRunner();
    let prompt = createGenerationPrompt(request);
    let lastError: Error | null = null;

    for (let attempt = 0; attempt <= this.maxRepairAttempts; attempt += 1) {
      try {
        const structured = parseStructuredOutput(
          await runStructuredQuery(runner, prompt, this.createQueryOptions())
        );
        const dependencyManifestArtifact = createDependencyManifestArtifact({
          packageManager: structured.packageManager,
          dependencies: structured.dependencies,
          devDependencies: structured.devDependencies,
          installCommand: structured.installCommand
        });
        const dependencyManifest = dependencyManifestFromArtifact(dependencyManifestArtifact, {
          packageManager: structured.packageManager,
          dependencies: structured.dependencies,
          devDependencies: structured.devDependencies,
          installCommand: structured.installCommand
        });
        assertDependencyManifestPolicy(dependencyManifest);

        const sourceArtifact = createGeneratedArtifact({
          path: `generated/${request.nodeId}.ts`,
          content: structured.sourceCode.endsWith("\n")
            ? structured.sourceCode
            : `${structured.sourceCode}\n`,
          contentType: "text/typescript"
        });

        return {
          sourceArtifact,
          dependencyManifestArtifact,
          dependencyManifest,
          metadata: createCodegenMetadata({
            generator: "anthropic.claude-agent-sdk",
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
      } catch (error) {
        lastError = error instanceof Error ? error : new Error("Code generation failed.");
        prompt = createRepairPrompt(request, lastError.message);
      }
    }

    throw lastError ?? new Error("Code generation failed.");
  }

  private async getQueryRunner(): Promise<AgentQueryRunner> {
    if (this.queryRunner) {
      return this.queryRunner;
    }
    if (!this.apiKey) {
      throw new Error("ANTHROPIC_API_KEY is required for live code generation.");
    }

    const sdk = await import("@anthropic-ai/claude-agent-sdk");
    return (prompt, options) => sdk.query({ prompt, options });
  }

  private createQueryOptions(): Options {
    const options: Options = {
      maxTurns: 1,
      tools: [],
      env: {
        ...process.env,
        ANTHROPIC_API_KEY: this.apiKey,
        CLAUDE_AGENT_SDK_CLIENT_APP: "kelpclaw-codegen/0.1.0"
      },
      outputFormat: {
        type: "json_schema",
        schema: generatedCodeSchema
      }
    };
    if (this.model) {
      options.model = this.model;
    }

    return options;
  }
}

async function runStructuredQuery(
  runner: AgentQueryRunner,
  prompt: string,
  options: Options
): Promise<unknown> {
  let result: unknown;
  for await (const message of runner(prompt, options)) {
    if (message.type === "result") {
      result = message.structured_output ?? message.result;
    }
  }

  if (result === undefined) {
    throw new Error("Anthropic Agent SDK did not return a structured result.");
  }

  return result;
}

function parseStructuredOutput(output: unknown): AgentStructuredOutput {
  const parsed = typeof output === "string" ? safeParseJson(output) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Generated code output must be a JSON object.");
  }

  const record = parsed as Partial<AgentStructuredOutput>;
  if (
    typeof record.sourceCode !== "string" ||
    !["none", "npm", "pnpm"].includes(String(record.packageManager)) ||
    !Array.isArray(record.dependencies) ||
    !Array.isArray(record.devDependencies) ||
    !Array.isArray(record.installCommand) ||
    !record.dependencies.every((dependency) => typeof dependency === "string") ||
    !record.devDependencies.every((dependency) => typeof dependency === "string") ||
    !record.installCommand.every((part) => typeof part === "string")
  ) {
    throw new Error("Generated code output does not match the required schema.");
  }

  return record as AgentStructuredOutput;
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    throw new Error("Generated code output was not valid JSON.");
  }
}

function createGenerationPrompt(request: CodegenGenerationRequest): string {
  return [
    "Generate a deterministic TypeScript NanoClaw node implementation.",
    "Return only structured output that matches the requested JSON schema.",
    "The code must read JSON from process.env.NANOCLAW_NODE_INPUT and write a JSON object to process.env.NANOCLAW_NODE_OUTPUT.",
    "Do not include undeclared network calls, filesystem reads outside the node workspace, secrets, replanning, or workflow mutation.",
    `Workflow id: ${request.workflowId}`,
    `Node id: ${request.nodeId}`,
    `Node prompt: ${request.prompt}`,
    `Planner rationale: ${request.plannerRationale}`,
    `Inputs JSON Schema: ${JSON.stringify(request.inputSchema)}`,
    `Outputs JSON Schema: ${JSON.stringify(request.outputSchema)}`,
    `Sandbox: ${JSON.stringify(request.sandbox)}`,
    `Runtime image: ${request.runtime.image}`
  ].join("\n");
}

function createRepairPrompt(request: CodegenGenerationRequest, error: string): string {
  return [
    createGenerationPrompt(request),
    "",
    "Repair the previous generated output.",
    `Validation error: ${error}`
  ].join("\n");
}

const generatedCodeSchema = {
  type: "object",
  required: ["sourceCode", "packageManager", "dependencies", "devDependencies", "installCommand"],
  additionalProperties: false,
  properties: {
    sourceCode: { type: "string", minLength: 1 },
    packageManager: { enum: ["none", "npm", "pnpm"] },
    dependencies: {
      type: "array",
      items: { type: "string", minLength: 1 }
    },
    devDependencies: {
      type: "array",
      items: { type: "string", minLength: 1 }
    },
    installCommand: {
      type: "array",
      items: { type: "string", minLength: 1 }
    }
  }
} as const;
