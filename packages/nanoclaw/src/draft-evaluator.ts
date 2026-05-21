import { createDefaultMockAdapters } from "@kelpclaw/adapters";
import { AdapterBackedNodeRunner } from "./adapter-runner.js";
import { compileDraftWorkflowDag } from "./compiler.js";
import { DeterministicNodeRunner } from "./deterministic-runner.js";
import { DockerNodeRunner } from "./docker-runner.js";
import { executeCompiledDag } from "./executor.js";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type {
  JsonRecord,
  WorkflowDraftEvaluation,
  WorkflowDraftEvaluationFinding,
  WorkflowRunEvent,
  WorkflowSpec,
  WorkflowValidationIssue
} from "@kelpclaw/workflow-spec";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";
import type { SecretResolver, SecretResolutionContext } from "./secrets.js";

export interface DraftWorkflowEvaluationOptions {
  readonly draftRevisionId?: string | undefined;
  readonly branchId?: string | undefined;
  readonly jobId?: string | undefined;
  readonly workspaceRoot?: string | undefined;
  readonly codegenArtifactStore?: CodegenArtifactStore | undefined;
  readonly runGeneratedNodesInDocker?: boolean | undefined;
  readonly dockerBin?: string | undefined;
  readonly hostWorkspace?: string | undefined;
  readonly runner?: NodeRunner | undefined;
  readonly startedAt?: string | undefined;
}

export async function evaluateDraftWorkflow(
  workflow: WorkflowSpec,
  options: DraftWorkflowEvaluationOptions = {}
): Promise<WorkflowDraftEvaluation> {
  const startedAt = options.startedAt ?? new Date().toISOString();
  const events: WorkflowRunEvent[] = [
    createEvaluationEvent("event.draft.started", startedAt, "info", "Draft evaluation started.")
  ];
  const secretIssues = validateDraftSecretRefs(workflow);
  if (secretIssues.length > 0) {
    return createEvaluation({
      workflow,
      options,
      startedAt,
      status: "failed",
      events,
      findings: secretIssues.map((issue, index) => ({
        id: `finding.secret.${index}`,
        severity: "error",
        target: { kind: "workflow" },
        message: issue.message,
        issues: [issue]
      }))
    });
  }

  try {
    const dag = compileDraftWorkflowDag(workflow, startedAt);
    const runner =
      options.runner ??
      new AdapterBackedNodeRunner({
        adapters: createDefaultMockAdapters(),
        fallbackRunner: new DraftFallbackRunner(options)
      });
    const result = await executeCompiledDag(dag, runner, {
      workspaceRoot: options.workspaceRoot,
      codegenArtifactStore: options.codegenArtifactStore,
      secretResolver: new DraftSecretResolver()
    });
    const findings = result.nodeResults
      .filter((nodeResult) => nodeResult.status === "failed")
      .map<WorkflowDraftEvaluationFinding>((nodeResult, index) => ({
        id: `finding.node.${index}`,
        severity: "error",
        target: {
          kind: "node",
          id: nodeResult.nodeId
        },
        message: nodeResult.error ?? `Node '${nodeResult.nodeId}' failed draft evaluation.`,
        issues: []
      }));

    return createEvaluation({
      workflow,
      options,
      startedAt,
      status: result.status === "succeeded" && findings.length === 0 ? "passed" : "failed",
      events: [...events, ...(result.events ?? [])],
      findings
    });
  } catch (error) {
    const issue: WorkflowValidationIssue = {
      code: "WORKFLOW_SCHEMA_INVALID",
      message: error instanceof Error ? error.message : "Draft workflow evaluation failed.",
      path: ["workflow"]
    };

    return createEvaluation({
      workflow,
      options,
      startedAt,
      status: "failed",
      events,
      findings: [
        {
          id: "finding.workflow.invalid",
          severity: "error",
          target: { kind: "workflow" },
          message: issue.message,
          issues: [issue]
        }
      ]
    });
  }
}

class DraftFallbackRunner implements NodeRunner {
  private readonly deterministicRunner = new DeterministicNodeRunner();
  private readonly dockerRunner: DockerNodeRunner | null;

  public constructor(options: DraftWorkflowEvaluationOptions) {
    this.dockerRunner =
      options.runGeneratedNodesInDocker !== false
        ? new DockerNodeRunner({
            dockerBin: options.dockerBin,
            hostWorkspace: options.hostWorkspace ?? process.cwd()
          })
        : null;
  }

  public run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    if (node.kind === "codegen" && this.dockerRunner) {
      return this.dockerRunner.run(node, context);
    }

    return this.deterministicRunner.run(node, context);
  }
}

class DraftSecretResolver implements SecretResolver {
  public async resolve(secretRef: string, context: SecretResolutionContext): Promise<string> {
    if (!secretRef.startsWith("secret:")) {
      throw new Error(`Draft evaluation refused raw secret for '${context.secretName}'.`);
    }

    return `mock:${context.secretName}`;
  }
}

function createEvaluation(input: {
  readonly workflow: WorkflowSpec;
  readonly options: DraftWorkflowEvaluationOptions;
  readonly startedAt: string;
  readonly status: WorkflowDraftEvaluation["status"];
  readonly events: readonly WorkflowRunEvent[];
  readonly findings: readonly WorkflowDraftEvaluationFinding[];
}): WorkflowDraftEvaluation {
  const finishedAt = new Date().toISOString();
  const statusEvent = createEvaluationEvent(
    "event.draft.finished",
    finishedAt,
    input.status === "passed" ? "info" : "error",
    "Draft evaluation finished."
  );

  return {
    id: `eval.${input.workflow.id}.r${input.workflow.revision}.${Date.now()}`,
    workflowId: input.workflow.id,
    branchId: input.options.branchId,
    draftRevisionId:
      input.options.draftRevisionId ?? `draft.${input.workflow.id}.r${input.workflow.revision}`,
    jobId: input.options.jobId,
    status: input.status,
    readyForApproval: input.status === "passed",
    createdAt: input.startedAt,
    finishedAt,
    mode: "draft",
    mockOnly: true,
    liveProviderCalls: 0,
    findings: input.findings,
    events: [...input.events, statusEvent],
    suggestions: input.findings.map((finding, index) => ({
      id: `suggestion.${input.workflow.id}.draft.${index}`,
      status: "suggested",
      conflict: "needs-repair",
      target:
        finding.target.kind === "artifact"
          ? { kind: "workflow" }
          : {
              kind: finding.target.kind,
              id: finding.target.id
            },
      title: "Draft evaluation finding",
      message: finding.message,
      issues: finding.issues
    }))
  };
}

function validateDraftSecretRefs(workflow: WorkflowSpec): readonly WorkflowValidationIssue[] {
  const issues: WorkflowValidationIssue[] = [];
  workflow.nodes.forEach((node, nodeIndex) => {
    Object.entries(node.secretRefs ?? {}).forEach(([secretName, secretRef]) => {
      if (!secretRef.startsWith("secret:")) {
        issues.push({
          code: "WORKFLOW_ADAPTER_SECRET_MISSING",
          message: `Draft evaluation refuses raw secret '${secretName}' on node '${node.id}'.`,
          path: ["nodes", nodeIndex, "secretRefs", secretName]
        });
      }
    });
  });

  return issues;
}

function createEvaluationEvent(
  id: string,
  timestamp: string,
  level: WorkflowRunEvent["level"],
  message: string,
  metadata?: JsonRecord | undefined
): WorkflowRunEvent {
  return {
    id,
    timestamp,
    level,
    message,
    kind: "draft.evaluation",
    ...(metadata ? { metadata } : {})
  };
}
