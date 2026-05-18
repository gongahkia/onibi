import { spawn } from "node:child_process";
import type { CompiledDagNode, NodeExecutionResult, NodeRunner } from "./types.js";

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

  public buildCommand(node: CompiledDagNode): readonly string[] {
    const envArgs = Object.entries(node.runtime.environment).flatMap(([key, value]) => [
      "--env",
      `${key}=${value}`
    ]);

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

  public async run(node: CompiledDagNode): Promise<NodeExecutionResult> {
    const startedAt = new Date().toISOString();
    const [command, ...args] = this.buildCommand(node);
    if (!command) {
      throw new Error("Docker command construction returned an empty command.");
    }

    const exitCode = await new Promise<number>((resolve, reject) => {
      const child = spawn(command, args, {
        cwd: this.hostWorkspace,
        stdio: "inherit"
      });
      child.on("error", reject);
      child.on("close", (code) => resolve(code ?? 1));
    });

    return {
      nodeId: node.id,
      status: exitCode === 0 ? "succeeded" : "failed",
      startedAt,
      finishedAt: new Date().toISOString(),
      output: {
        exitCode
      }
    };
  }
}
