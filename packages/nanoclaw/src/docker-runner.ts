import { spawn } from "node:child_process";
import { readdir, readFile, writeFile } from "node:fs/promises";
import { join, relative } from "node:path";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";
import type { JsonRecord } from "@kelpclaw/workflow-spec";

export interface DockerNodeRunnerOptions {
  readonly dockerBin?: string | undefined;
  readonly hostWorkspace: string;
  readonly containerWorkspace?: string | undefined;
}

export class DockerNodeRunner implements NodeRunner {
  private readonly dockerBin: string;
  private readonly hostWorkspace: string;
  private readonly containerWorkspace: string;

  public constructor(options: DockerNodeRunnerOptions) {
    this.dockerBin = options.dockerBin ?? "docker";
    this.hostWorkspace = options.hostWorkspace;
    this.containerWorkspace = options.containerWorkspace ?? "/workspace";
  }

  public buildCommand(
    node: CompiledDagNode,
    context?: Pick<NodeRunContext, "attempt" | "resolvedSecrets" | "workspace">
  ): readonly string[] {
    const nanoclawEnv: [string, string][] = context
      ? [
          ["NANOCLAW_WORKFLOW_SPEC", "/workflow/workflow.json"],
          ["NANOCLAW_NODE_INPUT", `${this.containerWorkspace}/input.json`],
          ["NANOCLAW_NODE_OUTPUT", `${this.containerWorkspace}/output.json`],
          ["NANOCLAW_ARTIFACTS_DIR", `${this.containerWorkspace}/artifacts`],
          ["NANOCLAW_NODE_ID", node.id],
          ["NANOCLAW_ATTEMPT", String(context.attempt)]
        ]
      : [];
    const resolvedSecrets = context ? Object.entries(context.resolvedSecrets) : [];
    const envArgs = [
      ...Object.entries(node.runtime.environment),
      ...resolvedSecrets,
      ...nanoclawEnv
    ]
      .sort(([leftKey], [rightKey]) => leftKey.localeCompare(rightKey))
      .flatMap(([key, value]) => ["--env", `${key}=${value}`]);

    if (!context) {
      return [
        this.dockerBin,
        "run",
        "--rm",
        "--network",
        "none",
        "--volume",
        `${this.hostWorkspace}:${this.containerWorkspace}`,
        "--workdir",
        this.containerWorkspace,
        ...envArgs,
        node.runtime.image,
        ...node.runtime.command
      ];
    }

    return [
      this.dockerBin,
      "run",
      "--rm",
      "--network",
      dockerNetworkForNode(node),
      "--cpus",
      node.runtime.resources.cpu,
      "--memory",
      `${node.runtime.resources.memoryMb}m`,
      "--volume",
      `${context.workspace.workflowSpecPath}:/workflow/workflow.json:ro`,
      "--volume",
      `${context.workspace.attemptDir}:${this.containerWorkspace}:rw`,
      "--workdir",
      this.containerWorkspace,
      ...envArgs,
      node.runtime.image,
      ...node.runtime.command
    ];
  }

  public async run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    const [command, ...args] = this.buildCommand(node, context);
    if (!command) {
      throw new Error("Docker command construction returned an empty command.");
    }

    const { exitCode, stdout, stderr } = await new Promise<{
      readonly exitCode: number;
      readonly stdout: string;
      readonly stderr: string;
    }>((resolve, reject) => {
      const child = spawn(command, args, {
        cwd: context.workspace.attemptDir,
        stdio: ["ignore", "pipe", "pipe"],
        signal: context.signal
      });
      const stdoutChunks: Buffer[] = [];
      const stderrChunks: Buffer[] = [];
      child.stdout.on("data", (chunk: Buffer) => stdoutChunks.push(chunk));
      child.stderr.on("data", (chunk: Buffer) => stderrChunks.push(chunk));
      child.on("error", reject);
      child.on("close", (code) =>
        resolve({
          exitCode: code ?? 1,
          stdout: Buffer.concat(stdoutChunks).toString("utf8"),
          stderr: Buffer.concat(stderrChunks).toString("utf8")
        })
      );
    });
    await writeFile(context.workspace.stdoutPath, stdout, "utf8");
    await writeFile(context.workspace.stderrPath, stderr, "utf8");

    const outputRead = await readOutputPayload(context.workspace.outputPath);
    const artifacts = await listArtifacts(context.workspace.artifactsDir);
    const status = exitCode === 0 && outputRead.ok ? "succeeded" : "failed";

    return {
      status,
      output: outputRead.ok ? outputRead.output : {},
      exitCode,
      error: outputRead.ok ? undefined : outputRead.error,
      stdoutPath: context.workspace.stdoutPath,
      stderrPath: context.workspace.stderrPath,
      artifacts,
      metadata: {
        exitCode,
        network: dockerNetworkForNode(node)
      }
    };
  }
}

async function readOutputPayload(
  outputPath: string
): Promise<
  | { readonly ok: true; readonly output: JsonRecord }
  | { readonly ok: false; readonly error: string }
> {
  try {
    const parsed: unknown = JSON.parse(await readFile(outputPath, "utf8"));
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {
        ok: false,
        error: `Node output '${outputPath}' must be a JSON object.`
      };
    }

    return {
      ok: true,
      output: parsed as JsonRecord
    };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : `Unable to read node output '${outputPath}'.`
    };
  }
}

async function listArtifacts(artifactsDir: string): Promise<readonly string[]> {
  return listArtifactsInDirectory(artifactsDir, artifactsDir);
}

async function listArtifactsInDirectory(
  rootDir: string,
  currentDir: string
): Promise<readonly string[]> {
  const entries = await readdir(currentDir, { withFileTypes: true });
  const artifacts: string[] = [];

  for (const entry of entries) {
    const absolutePath = join(currentDir, entry.name);
    if (entry.isDirectory()) {
      artifacts.push(...(await listArtifactsInDirectory(rootDir, absolutePath)));
    } else if (entry.isFile()) {
      artifacts.push(relative(rootDir, absolutePath));
    }
  }

  return artifacts.sort();
}

function dockerNetworkForNode(node: CompiledDagNode): "bridge" | "none" {
  if (node.codegen) {
    return node.codegen.sandbox.network === "declared" ? "bridge" : "none";
  }

  return node.determinism.externalCalls.length > 0 || node.adapterId ? "bridge" : "none";
}
