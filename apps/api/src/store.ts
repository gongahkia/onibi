import type { DagExecutionResult } from "@kelpclaw/nanoclaw";
import type { WorkflowSpec, WorkflowValidationResult } from "@kelpclaw/workflow-spec";

export type ApprovalDecision = "pending" | "approved" | "rejected";

export interface StoredWorkflow {
  readonly workflow: WorkflowSpec;
  readonly validation: WorkflowValidationResult;
  readonly approvals: Readonly<Record<string, ApprovalDecision>>;
  readonly createdAt: string;
}

export interface StoredExecution {
  readonly id: string;
  readonly workflowId: string;
  readonly createdAt: string;
  readonly result: DagExecutionResult;
}

export class InMemoryWorkflowStore {
  private readonly workflows = new Map<string, StoredWorkflow>();
  private readonly executions = new Map<string, StoredExecution>();

  public saveWorkflow(
    workflow: WorkflowSpec,
    validation: WorkflowValidationResult
  ): StoredWorkflow {
    const approvals = Object.fromEntries(
      (workflow.approvals ?? []).map((approval) => [approval.id, "pending" as const])
    );
    const stored = {
      workflow,
      validation,
      approvals,
      createdAt: new Date().toISOString()
    };

    this.workflows.set(workflow.metadata.id, stored);
    return stored;
  }

  public getWorkflow(id: string): StoredWorkflow | undefined {
    return this.workflows.get(id);
  }

  public setApproval(
    workflowId: string,
    approvalId: string,
    decision: Exclude<ApprovalDecision, "pending">
  ): StoredWorkflow {
    const stored = this.requireWorkflow(workflowId);
    if (!(approvalId in stored.approvals)) {
      throw new Error(`Unknown approval gate '${approvalId}'.`);
    }

    const updated = {
      ...stored,
      approvals: {
        ...stored.approvals,
        [approvalId]: decision
      }
    };
    this.workflows.set(workflowId, updated);

    return updated;
  }

  public approvalsSatisfied(workflowId: string): boolean {
    const stored = this.requireWorkflow(workflowId);
    return Object.values(stored.approvals).every((decision) => decision === "approved");
  }

  public saveExecution(execution: StoredExecution): StoredExecution {
    this.executions.set(execution.id, execution);
    return execution;
  }

  public getExecution(id: string): StoredExecution | undefined {
    return this.executions.get(id);
  }

  public requireWorkflow(id: string): StoredWorkflow {
    const stored = this.workflows.get(id);
    if (!stored) {
      throw new Error(`Unknown workflow '${id}'.`);
    }

    return stored;
  }
}
