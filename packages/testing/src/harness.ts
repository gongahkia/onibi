import { createDefaultMockAdapters } from "@kelpclaw/adapters";
import {
  AdapterBackedNodeRunner,
  compileWorkflowDag,
  executeCompiledDag
} from "@kelpclaw/nanoclaw";
import { approvedGmailReceiptsToSheetsWorkflowFixture } from "@kelpclaw/workflow-spec";
import type { MockAdapter } from "@kelpclaw/adapters";
import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";

export interface DeterministicHarness {
  readonly workflow: WorkflowSpec;
  readonly adapters: ReadonlyMap<string, MockAdapter>;
  runWorkflow(workflow?: WorkflowSpec): Promise<DagExecutionResult>;
}

export function createDeterministicHarness(
  workflow: WorkflowSpec = approvedGmailReceiptsToSheetsWorkflowFixture
): DeterministicHarness {
  const adapters = createDefaultMockAdapters();

  return {
    workflow,
    adapters,
    async runWorkflow(workflowOverride = workflow) {
      const dag = compileWorkflowDag(workflowOverride);
      return executeCompiledDag(dag, new AdapterBackedNodeRunner({ adapters }), {
        secretResolver: deterministicSecretResolver
      });
    }
  };
}

export async function runStaticFixture(): Promise<DagExecutionResult> {
  return createDeterministicHarness(approvedGmailReceiptsToSheetsWorkflowFixture).runWorkflow();
}

const deterministicSecretResolver = {
  async resolve(secretRef: string): Promise<string> {
    if (secretRef === "secret:google.oauth.default") {
      return JSON.stringify({ accessToken: "test-google-access-token" });
    }
    if (secretRef === "secret:email.smtp.default") {
      return JSON.stringify({ host: "smtp.test", from: "owner@example.com" });
    }

    return secretRef;
  }
};
