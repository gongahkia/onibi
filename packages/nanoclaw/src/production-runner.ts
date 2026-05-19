import { createDefaultLiveAdapters } from "@kelpclaw/adapters";
import { declaredAdapterOperations } from "./adapter-policy.js";
import { AdapterBackedNodeRunner } from "./adapter-runner.js";
import { DeterministicNodeRunner } from "./deterministic-runner.js";
import { DockerNodeRunner } from "./docker-runner.js";
import type { Adapter } from "@kelpclaw/adapters";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";

export interface ProductionNodeRunnerOptions {
  readonly adapters?: ReadonlyMap<string, Adapter> | undefined;
  readonly dockerBin?: string | undefined;
  readonly hostWorkspace: string;
  readonly containerWorkspace?: string | undefined;
}

export class ProductionNodeRunner implements NodeRunner {
  private readonly adapterRunner: AdapterBackedNodeRunner;

  public constructor(options: ProductionNodeRunnerOptions) {
    this.adapterRunner = new AdapterBackedNodeRunner({
      adapters: options.adapters ?? createDefaultLiveAdapters(),
      fallbackRunner: new ProductionFallbackNodeRunner(options)
    });
  }

  public run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    return this.adapterRunner.run(node, context);
  }
}

class ProductionFallbackNodeRunner implements NodeRunner {
  private readonly deterministicRunner = new DeterministicNodeRunner();
  private readonly dockerRunner: DockerNodeRunner;

  public constructor(options: ProductionNodeRunnerOptions) {
    this.dockerRunner = new DockerNodeRunner({
      dockerBin: options.dockerBin,
      hostWorkspace: options.hostWorkspace,
      containerWorkspace: options.containerWorkspace
    });
  }

  public run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    if (declaredAdapterOperations(node).length > 0 || node.kind === "codegen") {
      return this.dockerRunner.run(node, context);
    }

    return this.deterministicRunner.run(node, context);
  }
}
