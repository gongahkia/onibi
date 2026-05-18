import { createDefaultFakeAdapters } from "@kelpclaw/adapters";
import { MockNodeRunner, compileWorkflowDag, executeCompiledDag } from "@kelpclaw/nanoclaw";
import { staticContentWorkflowFixture } from "@kelpclaw/workflow-spec";
import type { FakeAdapter } from "@kelpclaw/adapters";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";

export interface DeterministicHarness {
  readonly workflow: WorkflowSpec;
  readonly adapters: ReadonlyMap<string, FakeAdapter>;
  runWorkflow(workflow?: WorkflowSpec): Promise<DagExecutionResult>;
}

export function createDeterministicHarness(
  workflow: WorkflowSpec = staticContentWorkflowFixture
): DeterministicHarness {
  const adapters = createDefaultFakeAdapters();

  return {
    workflow,
    adapters,
    async runWorkflow(workflowOverride = workflow) {
      const dag = compileWorkflowDag(workflowOverride);
      return executeCompiledDag(dag, new MockNodeRunner());
    }
  };
}

export async function runStaticFixture(): Promise<DagExecutionResult> {
  return createDeterministicHarness(staticContentWorkflowFixture).runWorkflow();
}
