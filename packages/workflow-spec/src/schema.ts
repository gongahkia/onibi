import { z } from "zod";
import {
  agentStepClassifications,
  agentStepSourceAgents,
  agentStepStatuses,
  workflowSchemaVersion,
  workflowValidationErrorCodes
} from "./types.js";
import type { JsonRecord, JsonValue } from "./types.js";

export const jsonValueSchema: z.ZodType<JsonValue> = z.lazy(
  () =>
    z.union([
      z.string(),
      z.number().finite(),
      z.boolean(),
      z.null(),
      z.array(jsonValueSchema),
      z.record(z.string(), jsonValueSchema)
    ]) as z.ZodType<JsonValue>
);

export const jsonRecordSchema: z.ZodType<JsonRecord> = z.record(z.string(), jsonValueSchema);
export const jsonSchemaShapeSchema: z.ZodType<JsonRecord> = jsonRecordSchema;

export const workflowRuntimeRetrySchema = z.object({
  maxAttempts: z.number().int().min(0),
  backoffSeconds: z.number().min(0)
});

export const workflowRuntimeResourcesSchema = z.object({
  cpu: z.string().min(1),
  memoryMb: z.number().int().positive()
});

export const workflowRuntimeSchema = z.object({
  image: z.string().min(1),
  command: z.array(z.string().min(1)).min(1),
  timeoutSeconds: z.number().int().positive(),
  retry: workflowRuntimeRetrySchema,
  environment: z.record(z.string(), z.string()),
  resources: workflowRuntimeResourcesSchema
});

export const workflowDeterminismSchema = z.object({
  externalCalls: z.array(z.string().min(1)),
  seededRandomness: z.object({
    enabled: z.boolean(),
    seed: z.string().min(1).optional()
  }),
  replayBehavior: z.enum(["none", "record", "replay", "reuse-if-unchanged", "fail-on-drift"])
});

export const workflowCodegenMetadataSchema = z.object({
  originalPrompt: z.string().min(1),
  latestPrompt: z.string().min(1),
  plannerRationale: z.string().min(1),
  provenance: z.object({
    generator: z.string().min(1),
    generatedAt: z.string().datetime(),
    sourcePrompt: z.string().min(1),
    artifactPath: z.string().min(1),
    artifactChecksum: z.string().regex(/^sha256:[a-f0-9]{64}$/)
  }),
  artifacts: z
    .array(
      z.object({
        path: z.string().min(1),
        checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
        contentType: z.enum(["application/json", "text/markdown", "text/plain", "text/typescript"])
      })
    )
    .min(1),
  dependencyManifest: z.object({
    path: z.string().min(1),
    checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
    packageManager: z.enum(["none", "npm", "pnpm"]),
    dependencies: z.array(z.string().min(1)),
    devDependencies: z.array(z.string().min(1)),
    installCommand: z.array(z.string().min(1))
  }),
  sandbox: z.object({
    network: z.enum(["none", "declared"]),
    allowedHosts: z.array(z.string().min(1)),
    mounts: z.array(
      z.object({
        source: z.string().min(1),
        target: z.string().min(1),
        mode: z.enum(["ro", "rw"])
      })
    ),
    resources: workflowRuntimeResourcesSchema
  }),
  review: z.object({
    status: z.enum(["draft", "approved", "rejected"]),
    reviewedBy: z.string().min(1).optional(),
    reviewedAt: z.string().datetime().optional(),
    notes: z.string().optional()
  }),
  replay: z.object({
    mode: z.enum(["reuse-if-unchanged", "always-regenerate", "fail-on-drift"]),
    seed: z.string().min(1)
  }),
  llmBacked: z.boolean()
});

export const workflowArtifactRefSchema = z.object({
  path: z.string().min(1),
  checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  contentType: z.enum(["application/json", "text/markdown", "text/plain", "text/typescript"])
});

export const workflowAgentBudgetSchema = z.object({
  maxIterations: z.number().int().min(0),
  maxWallClockSeconds: z.number().int().positive(),
  maxModelCostUsd: z.number().min(0),
  maxDockerRuntimeSeconds: z.number().int().min(0),
  maxRetries: z.number().int().min(0)
});

export const workflowAgenticToolGrantSchema = z.object({
  kind: z.enum(["builtin", "mcp", "adapter"]),
  name: z.string().min(1),
  connectorId: z.string().min(1).optional(),
  adapterId: z.string().min(1).optional(),
  operation: z.string().min(1).optional(),
  operationVersion: z.string().min(1).optional(),
  allowedHosts: z.array(z.string().min(1)),
  secretRefs: z.array(z.string().min(1)),
  sideEffect: z.enum(["none", "read", "write"])
});

export const workflowAgenticNodePolicySchema = z.object({
  tools: z.array(z.string().min(1)),
  toolGrants: z.array(workflowAgenticToolGrantSchema).optional(),
  memoryScope: z.enum(["none", "node", "workflow", "workspace"]),
  stopConditions: z.array(z.string().min(1)),
  humanApprovalBoundaries: z.array(z.string().min(1)),
  networkPolicy: z.enum(["none", "declared"]),
  allowedHosts: z.array(z.string().min(1)),
  secretRefs: z.array(z.string().min(1)),
  evalContract: jsonRecordSchema,
  budget: workflowAgentBudgetSchema
});

export const workflowAdapterOperationRefSchema = z.object({
  adapterId: z.string().min(1),
  operation: z.string().min(1),
  operationVersion: z.string().min(1)
});

export const workflowNodeCompensationSchema = z.object({
  strategy: z.enum(["none", "manual", "adapter-operation"]),
  adapterOperation: workflowAdapterOperationRefSchema.optional(),
  inputFrom: z.enum(["node-input", "node-output", "config"]).optional(),
  instructions: z.string().min(1).optional()
});

export const agentStepMetadataSchema = z.object({
  sourceAgent: z.enum(agentStepSourceAgents),
  sessionId: z.string().min(1),
  hookEvent: z.string().min(1),
  toolName: z.string().min(1),
  toolUseId: z.string().min(1),
  parentToolUseId: z.string().min(1).optional(),
  args: jsonRecordSchema,
  result: jsonValueSchema.optional(),
  status: z.enum(agentStepStatuses),
  contentHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  prevEventHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  chainIndex: z.number().int().min(0),
  classification: z.enum(agentStepClassifications).optional(),
  startedAt: z.string().datetime(),
  finishedAt: z.string().datetime().optional()
});

export const workflowNodeSchema = z.object({
  id: z.string().min(1),
  kind: z.enum(["trigger", "skill", "codegen", "transform", "approval", "delivery", "agent-step"]),
  label: z.string().min(1),
  description: z.string().min(1),
  inputs: z.record(z.string(), jsonSchemaShapeSchema),
  outputs: z.record(z.string(), jsonSchemaShapeSchema),
  config: jsonRecordSchema,
  runtime: workflowRuntimeSchema,
  determinism: workflowDeterminismSchema,
  skillId: z.string().min(1).optional(),
  adapterId: z.string().min(1).optional(),
  adapterIds: z.array(z.string().min(1)).optional(),
  adapterOperations: z.array(workflowAdapterOperationRefSchema).optional(),
  secretRefs: z.record(z.string(), z.string().min(1)).optional(),
  codegen: workflowCodegenMetadataSchema.optional(),
  agentic: workflowAgenticNodePolicySchema.optional(),
  agentStep: agentStepMetadataSchema.optional(),
  compensation: workflowNodeCompensationSchema.optional()
});

export const workflowPortRefSchema = z.object({
  nodeId: z.string().min(1),
  port: z.string().min(1)
});

export const workflowEdgeSchema = z.object({
  id: z.string().min(1),
  source: workflowPortRefSchema,
  target: workflowPortRefSchema
});

export const workflowApprovalRecordSchema = z.object({
  status: z.literal("approved"),
  approvedBy: z.string().min(1),
  approvedAt: z.string().datetime(),
  frozenRevision: z.number().int().positive(),
  frozenDagHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  nodeOrder: z.array(z.string().min(1)).min(1)
});

export const workflowSpecSchema = z.object({
  id: z.string().min(1),
  schemaVersion: z.literal(workflowSchemaVersion),
  name: z.string().min(1),
  prompt: z.string().min(1),
  revision: z.number().int().positive(),
  nodes: z.array(workflowNodeSchema).min(1),
  edges: z.array(workflowEdgeSchema),
  approval: workflowApprovalRecordSchema.nullable(),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime()
});

export const workflowEventSeveritySchema = z.enum(["debug", "info", "warn", "error", "critical"]);

export const workflowObservabilityEventKindSchema = z.enum([
  "task.routing",
  "prompt.planning",
  "skill.matching",
  "draft.edit",
  "planner.feedback",
  "draft.evaluation",
  "node.reprompt",
  "workflow.approval",
  "job.lifecycle",
  "agent.activity",
  "budget.lifecycle",
  "node.decision",
  "workspace.artifact",
  "dag.compilation",
  "node.container",
  "adapter.call",
  "codegen.artifact",
  "delivery.event",
  "run.lifecycle",
  "deployment.lifecycle",
  "connector.lifecycle",
  "checkpoint.lifecycle",
  "schedule.lifecycle",
  "alert.lifecycle",
  "retention.lifecycle",
  "compensation.required"
]);

export const workflowObservabilityContextSchema = z.object({
  workflowId: z.string().min(1),
  revisionId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  runId: z.string().min(1).optional(),
  nodeId: z.string().min(1).optional(),
  correlationId: z.string().min(1)
});

export const workflowObservabilityEventSchema = workflowObservabilityContextSchema.extend({
  id: z.string().min(1),
  timestamp: z.string().datetime(),
  severity: workflowEventSeveritySchema,
  kind: workflowObservabilityEventKindSchema,
  message: z.string().min(1),
  metadata: jsonRecordSchema.optional()
});

export const workflowAuditActionSchema = z.enum([
  "workflow.created",
  "workflow.edited",
  "plan.accepted",
  "branch.created",
  "branch.updated",
  "branch.merged",
  "branch.cherry-picked",
  "codegen.reused",
  "workflow.approved",
  "codegen.reviewed",
  "task.routed",
  "planner.feedback.created",
  "planner.feedback.decided",
  "draft.evaluated",
  "job.created",
  "agent.ran",
  "decision.trace.recorded",
  "budget.updated",
  "budget.blocked",
  "workspace.created",
  "deployment.created",
  "deployment.undeployed",
  "deployment.rolled-back",
  "audit.exported",
  "trajectory.promoted",
  "policy.denied",
  "policy.approved",
  "tbom.exported",
  "secret.referenced",
  "container.ran",
  "adapter.called",
  "delivery.completed",
  "run.completed",
  "connector.created",
  "connector.deleted",
  "schedule.updated",
  "retention.cleaned"
]);

export const workflowSpecDiffLineSchema = z.object({
  kind: z.enum(["same", "added", "removed"]),
  text: z.string()
});

export const workflowSpecDiffSchema = z.object({
  changed: z.boolean(),
  summary: z.array(z.string()),
  lines: z.array(workflowSpecDiffLineSchema)
});

export const workflowAuditRecordSchema = workflowObservabilityContextSchema.extend({
  id: z.string().min(1),
  timestamp: z.string().datetime(),
  action: workflowAuditActionSchema,
  actor: z.string().min(1),
  summary: z.string().min(1),
  diff: workflowSpecDiffSchema.optional(),
  approvedArtifactRefs: z
    .array(
      z.object({
        path: z.string().min(1),
        checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
        contentType: z.enum(["application/json", "text/markdown", "text/plain", "text/typescript"])
      })
    )
    .optional(),
  secretRefs: z.array(z.string().min(1)).optional(),
  container: z
    .object({
      image: z.string().min(1),
      command: z.array(z.string().min(1)),
      network: z.enum(["none", "declared", "bridge"]),
      workspacePath: z.string().min(1).optional()
    })
    .optional(),
  adapterCall: z
    .object({
      adapterId: z.string().min(1),
      operation: z.string().min(1),
      operationVersion: z.string().min(1),
      status: z.enum(["succeeded", "failed"])
    })
    .optional(),
  delivery: z
    .object({
      channels: z.array(z.string().min(1)),
      status: z.enum(["succeeded", "failed"])
    })
    .optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowArtifactManifestRecordSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  revisionId: z.string().min(1),
  createdAt: z.string().datetime(),
  artifacts: z.array(
    z.object({
      path: z.string().min(1),
      checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
      contentType: z.enum(["application/json", "text/markdown", "text/plain", "text/typescript"])
    })
  ),
  manifestChecksum: z.string().regex(/^sha256:[a-f0-9]{64}$/)
});

export const workflowValidationIssueSchema = z.object({
  code: z.enum(workflowValidationErrorCodes),
  message: z.string().min(1),
  path: z.array(z.union([z.string(), z.number().int()]))
});

export const workflowValidationResultSchema = z.union([
  z.object({
    ok: z.literal(true),
    workflow: workflowSpecSchema
  }),
  z.object({
    ok: z.literal(false),
    errors: z.array(workflowValidationIssueSchema)
  })
]);

export const workflowAgentRoleSchema = z.enum([
  "classifier",
  "planner",
  "workflow-architect",
  "agentic-node-designer",
  "coder",
  "tester",
  "runner",
  "fixer",
  "evaluator",
  "summarizer"
]);

export const workflowRetryBudgetSchema = z.object({
  maxAttempts: z.number().int().min(0),
  maxCostUsd: z.number().min(0)
});

export const workflowModelRequirementSchema = z.object({
  mode: z.enum(["none", "deterministic", "live"]),
  role: workflowAgentRoleSchema,
  provider: z.string().min(1).optional(),
  model: z.string().min(1).optional(),
  retryBudget: workflowRetryBudgetSchema
});

export const workflowModelInvocationRecordSchema = z.object({
  id: z.string().min(1),
  role: workflowAgentRoleSchema,
  inputSummary: z.string().min(1),
  outputArtifact: z.string().min(1),
  provider: z.string().min(1),
  model: z.string().min(1),
  determinismExpectation: z.enum(["deterministic", "bounded", "non-deterministic"]),
  retryBudget: workflowRetryBudgetSchema,
  correlationId: z.string().min(1),
  createdAt: z.string().datetime(),
  durationMs: z.number().min(0).optional(),
  durationApiMs: z.number().min(0).optional(),
  inputTokens: z.number().int().min(0).optional(),
  outputTokens: z.number().int().min(0).optional(),
  cacheReadInputTokens: z.number().int().min(0).optional(),
  cacheCreationInputTokens: z.number().int().min(0).optional(),
  totalTokens: z.number().int().min(0).optional(),
  costUsd: z.number().min(0).optional(),
  modelUsage: jsonRecordSchema.optional(),
  failureReason: z.string().min(1).optional()
});

export const workflowLifecycleStageSchema = z.enum([
  "empty",
  "planned",
  "accepted",
  "generated",
  "evaluated",
  "approved",
  "deployed",
  "runnable"
]);

export const workflowRuntimeTruthSnapshotSchema = z.object({
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  stage: workflowLifecycleStageSchema,
  planned: z.boolean(),
  accepted: z.boolean(),
  generated: z.boolean(),
  evaluated: z.boolean(),
  approved: z.boolean(),
  deployed: z.boolean(),
  runnable: z.boolean(),
  draftRevisionId: z.string().min(1).optional(),
  acceptedDraftRevisionId: z.string().min(1).optional(),
  evaluationId: z.string().min(1).optional(),
  approvedRevisionId: z.string().min(1).optional(),
  runnerDeploymentId: z.string().min(1).optional(),
  activeDeploymentIds: z.array(z.string().min(1)),
  blockingReasons: z.array(z.string().min(1)),
  updatedAt: z.string().datetime()
});

export const workflowProviderRuntimeConfigSchema = z.object({
  role: z.enum([
    "planner",
    "agentic-research",
    "codegen",
    "workflow-architect",
    "coder",
    "tester",
    "runner",
    "fixer",
    "evaluator"
  ]),
  provider: z.enum(["anthropic", "openai", "openweight", "deterministic"]),
  model: z.string().min(1),
  configured: z.boolean(),
  missingCredential: z.string().min(1).optional(),
  tokenAccounting: z.boolean(),
  costAccounting: z.boolean(),
  retryBudget: workflowRetryBudgetSchema,
  runtimeLimits: jsonRecordSchema
});

export const workflowBudgetPolicySchema = z.object({
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  maxWorkflowCostUsd: z.number().min(0),
  maxCodegenCostUsd: z.number().min(0),
  maxAgenticCostUsd: z.number().min(0),
  expensiveRetryConfirmationUsd: z.number().min(0),
  perAgentMaxCostUsd: z.record(z.string(), z.number().min(0)),
  updatedAt: z.string().datetime(),
  updatedBy: z.string().min(1)
});

export const workflowBudgetLedgerSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  jobId: z.string().min(1).optional(),
  agentRunId: z.string().min(1).optional(),
  scope: z.enum(["workflow", "job", "agent"]),
  projectedCostUsd: z.number().min(0),
  actualCostUsd: z.number().min(0),
  remainingCostUsd: z.number(),
  retryEstimateUsd: z.number().min(0),
  status: z.enum(["within-budget", "confirmation-required", "blocked", "exhausted"]),
  stopReason: z.string().min(1).optional(),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime()
});

export const workflowAgentTimelineEventSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  jobId: z.string().min(1).optional(),
  nodeId: z.string().min(1).optional(),
  agentRunId: z.string().min(1).optional(),
  role: workflowAgentRoleSchema,
  timestamp: z.string().datetime(),
  status: z.enum(["started", "succeeded", "failed", "blocked"]),
  title: z.string().min(1),
  summary: z.string().min(1),
  decision: z.string().min(1).optional(),
  fixTriageAction: z.enum(["targeted-patch", "retry-codegen", "rearchitect", "give-up"]).optional(),
  outputArtifactRefs: z.array(workflowArtifactRefSchema),
  inputTokens: z.number().int().min(0).optional(),
  outputTokens: z.number().int().min(0).optional(),
  totalTokens: z.number().int().min(0).optional(),
  costUsd: z.number().min(0).optional(),
  cumulativeCostUsd: z.number().min(0),
  metadata: jsonRecordSchema.optional()
});

export const workflowDecisionTraceKindSchema = z.enum([
  "planner.node-created",
  "planner.node-updated",
  "planner.edge-designed",
  "codegen.architect",
  "codegen.coder",
  "codegen.tester",
  "codegen.runner",
  "codegen.fixer",
  "codegen.evaluator",
  "runtime.router-classification",
  "runtime.agent-policy",
  "runtime.tool-call",
  "runtime.memory-read",
  "runtime.memory-write"
]);

export const workflowNodeDecisionTraceEventSchema = z.object({
  id: z.string().min(1),
  traceId: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  nodeId: z.string().min(1),
  revisionId: z.string().min(1).optional(),
  jobId: z.string().min(1).optional(),
  agentRunId: z.string().min(1).optional(),
  kind: workflowDecisionTraceKindSchema,
  role: workflowAgentRoleSchema,
  createdAt: z.string().datetime(),
  summary: z.string().min(1),
  rationale: z.string().min(1),
  alternativesConsidered: z.array(z.string().min(1)),
  selectedAction: z.string().min(1),
  inputSummary: z.string().min(1),
  promptHash: z
    .string()
    .regex(/^sha256:[a-f0-9]{64}$/)
    .optional(),
  promptExcerpt: z.string().min(1).optional(),
  route: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]).optional(),
  provider: z.string().min(1).optional(),
  model: z.string().min(1).optional(),
  modelInvocationIds: z.array(z.string().min(1)),
  affectedNodeIds: z.array(z.string().min(1)),
  affectedEdgeIds: z.array(z.string().min(1)),
  constraints: jsonRecordSchema,
  outputArtifactRefs: z.array(workflowArtifactRefSchema),
  evalOutcome: z.enum(["passed", "failed", "blocked", "not-run"]).optional(),
  failureClass: z.string().min(1).optional(),
  fixTriageAction: z.enum(["targeted-patch", "retry-codegen", "rearchitect", "give-up"]).optional(),
  inputTokens: z.number().int().min(0).optional(),
  outputTokens: z.number().int().min(0).optional(),
  totalTokens: z.number().int().min(0).optional(),
  costUsd: z.number().min(0).optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowNodeDecisionTraceSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  nodeId: z.string().min(1),
  revisionId: z.string().min(1).optional(),
  kind: workflowDecisionTraceKindSchema,
  source: z.enum(["planner", "codegen", "runtime"]),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  status: z.enum(["recorded", "succeeded", "failed", "blocked"]),
  events: z.array(workflowNodeDecisionTraceEventSchema).min(1)
});

export const workflowDecisionTraceEvalExampleSchema = z.object({
  id: z.string().min(1),
  traceId: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  nodeId: z.string().min(1),
  kind: workflowDecisionTraceKindSchema,
  createdAt: z.string().datetime(),
  input: jsonRecordSchema,
  expectedDecision: z.string().min(1).optional(),
  actualDecision: z.string().min(1),
  outcome: z.enum(["pass", "fail", "blocked", "unknown"]),
  failureClass: z.string().min(1).optional(),
  artifactRefs: z.array(workflowArtifactRefSchema),
  metadata: jsonRecordSchema.optional()
});

export const workflowNodeDecisionTraceExportSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  exportedAt: z.string().datetime(),
  format: z.literal("jsonl"),
  redacted: z.literal(true),
  lineCount: z.number().int().min(0),
  records: z.array(jsonRecordSchema),
  evalExamples: z.array(workflowDecisionTraceEvalExampleSchema)
});

export const workflowTaskRouteSchema = z.object({
  route: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
  rationale: z.string().min(1),
  requiredModel: workflowModelRequirementSchema,
  expectedNodeKinds: z.array(
    z.enum(["trigger", "skill", "codegen", "transform", "approval", "delivery", "agent-step"])
  ),
  dockerSandboxRequired: z.boolean(),
  draftTestsRequired: z.boolean(),
  productionDeterministic: z.boolean(),
  modelInvocations: z.array(workflowModelInvocationRecordSchema),
  classifierVersion: z.string().min(1),
  confidence: z.number().min(0).max(1),
  scores: z.array(
    z.object({
      route: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
      score: z.number(),
      positiveSignals: z.array(z.string().min(1)),
      negativeSignals: z.array(z.string().min(1))
    })
  ),
  alternatives: z.array(
    z.object({
      route: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
      score: z.number(),
      reason: z.string().min(1),
      suppressed: z.boolean()
    })
  ),
  matchedSignals: z.array(z.string().min(1))
});

export const workflowRouterEvalCaseSchema = z.object({
  id: z.string().min(1),
  prompt: z.string().min(1),
  expectedRoute: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
  minConfidence: z.number().min(0).max(1),
  forceDeterministic: z.boolean().optional(),
  expectedNodeKinds: z
    .array(
      z.enum(["trigger", "skill", "codegen", "transform", "approval", "delivery", "agent-step"])
    )
    .optional()
});

export const workflowRouterEvalCaseResultSchema = z.object({
  id: z.string().min(1),
  prompt: z.string().min(1),
  expectedRoute: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
  actualRoute: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
  confidence: z.number().min(0).max(1),
  passed: z.boolean(),
  route: workflowTaskRouteSchema,
  failures: z.array(z.string().min(1))
});

export const workflowRouterEvalRunSchema = z.object({
  id: z.string().min(1),
  classifierVersion: z.string().min(1),
  createdAt: z.string().datetime(),
  passed: z.boolean(),
  total: z.number().int().min(0),
  failed: z.number().int().min(0),
  results: z.array(workflowRouterEvalCaseResultSchema)
});

export const workflowAgentMemoryRecordSchema = z.object({
  id: z.string().min(1),
  scope: z.enum(["node", "workflow", "workspace"]),
  namespace: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  runId: z.string().min(1).optional(),
  nodeId: z.string().min(1).optional(),
  tags: z.array(z.string().min(1)),
  contentHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  content: jsonRecordSchema,
  shareable: z.boolean(),
  sourceTraceId: z.string().min(1).optional(),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  expiresAt: z.string().datetime().optional()
});

export const workflowGraphChangeSchema = z.object({
  id: z.string().min(1),
  kind: z.enum([
    "node.added",
    "node.removed",
    "node.moved",
    "node.edited",
    "edge.added",
    "edge.removed",
    "edge.reconnected"
  ]),
  elementId: z.string().min(1),
  path: z.array(z.union([z.string(), z.number().int()])),
  before: jsonValueSchema.optional(),
  after: jsonValueSchema.optional()
});

export const workflowGraphDiffSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  baseRevision: z.number().int().positive(),
  editedRevision: z.number().int().positive(),
  createdAt: z.string().datetime(),
  summary: z.array(z.string()),
  changes: z.array(workflowGraphChangeSchema),
  validation: workflowValidationResultSchema
});

export const workflowPlannerSuggestionSchema = z.object({
  id: z.string().min(1),
  status: z.enum(["suggested", "accepted", "rejected"]),
  conflict: z.enum(["safe", "invalid", "under-specified", "needs-repair"]),
  target: z.object({
    kind: z.enum(["workflow", "node", "edge"]),
    id: z.string().min(1).optional()
  }),
  title: z.string().min(1),
  message: z.string().min(1),
  patch: jsonRecordSchema.optional(),
  issues: z.array(workflowValidationIssueSchema)
});

export const workflowPlannerFeedbackSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  graphDiffId: z.string().min(1),
  route: workflowTaskRouteSchema,
  createdAt: z.string().datetime(),
  status: z.enum(["ready", "warnings", "blocked"]),
  suggestions: z.array(workflowPlannerSuggestionSchema),
  issues: z.array(workflowValidationIssueSchema)
});

export const workflowBranchSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  name: z.string().min(1),
  status: z.enum(["active", "archived"]),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  createdBy: z.string().min(1),
  parentBranchId: z.string().min(1).optional(),
  baseDraftRevisionId: z.string().min(1),
  headDraftRevisionId: z.string().min(1),
  acceptedDraftRevisionId: z.string().min(1).optional(),
  latestApprovedRevisionId: z.string().min(1).optional(),
  latestDraftEvaluationId: z.string().min(1).optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowPromptTurnSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1),
  source: z.enum(["plan", "reprompt", "edit", "merge", "cherry-pick"]),
  prompt: z.string().min(1),
  actor: z.string().min(1),
  createdAt: z.string().datetime(),
  baseDraftRevisionId: z.string().min(1).optional(),
  resultingDraftRevisionId: z.string().min(1).optional(),
  route: workflowTaskRouteSchema.optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowUpdateBranchRequestSchema = z
  .object({
    name: z.string().optional(),
    status: z.enum(["active", "archived"]).optional(),
    updatedBy: z.string().min(1)
  })
  .refine((request) => request.name !== undefined || request.status !== undefined, {
    message: "Branch update must include a name or status."
  });

export const workflowUpdateBranchResponseSchema = z.object({
  ok: z.literal(true),
  branch: workflowBranchSchema
});

export const workflowBranchMergeConflictSchema = z.object({
  id: z.string().min(1),
  kind: z.enum([
    "both-edited",
    "delete-edit",
    "add-add-id-collision",
    "missing-edge-endpoint",
    "schema-drift",
    "runtime-drift",
    "codegen-drift",
    "validation-blocked"
  ]),
  elementKind: z.enum(["workflow", "node", "edge"]),
  elementId: z.string().min(1).optional(),
  path: z.array(z.union([z.string(), z.number().int()])),
  message: z.string().min(1),
  baseValue: jsonValueSchema.optional(),
  sourceValue: jsonValueSchema.optional(),
  targetValue: jsonValueSchema.optional()
});

export const workflowBranchMergeResolutionSchema = z.object({
  conflictId: z.string().min(1),
  choice: z.enum(["source", "target", "manual"]),
  value: jsonValueSchema.optional()
});

export const workflowBranchMergePreviewSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  sourceBranchId: z.string().min(1),
  targetBranchId: z.string().min(1),
  mode: z.enum(["merge", "cherry-pick"]),
  status: z.enum(["clean", "conflicts", "blocked"]),
  createdAt: z.string().datetime(),
  baseDraftRevisionId: z.string().min(1),
  sourceHeadDraftRevisionId: z.string().min(1),
  targetHeadDraftRevisionId: z.string().min(1),
  graphDiff: workflowGraphDiffSchema,
  conflicts: z.array(workflowBranchMergeConflictSchema),
  mergedWorkflow: workflowSpecSchema.optional(),
  validation: workflowValidationResultSchema,
  summary: z.array(z.string())
});

export const workflowBranchMergeRecordSchema = workflowBranchMergePreviewSchema.extend({
  status: z.enum(["clean", "conflicts", "blocked", "applied"]),
  appliedAt: z.string().datetime().optional(),
  appliedBy: z.string().min(1).optional(),
  mergedDraftRevisionId: z.string().min(1).optional(),
  resolutions: z.array(workflowBranchMergeResolutionSchema)
});

export const workflowJobEventSchema = z.object({
  id: z.string().min(1),
  jobId: z.string().min(1),
  timestamp: z.string().datetime(),
  level: z.enum(["info", "warn", "error"]),
  message: z.string().min(1),
  kind: workflowObservabilityEventKindSchema,
  metadata: jsonRecordSchema.optional()
});

export const workflowJobSchema = z.object({
  id: z.string().min(1),
  type: z.enum([
    "plan.workflow",
    "feedback.graph",
    "evaluate.draft",
    "build.codegen-node",
    "test.codegen-node",
    "approve.workflow",
    "run.workflow",
    "deploy.workflow",
    "smoke.integration"
  ]),
  status: z.enum(["queued", "running", "succeeded", "failed", "cancelled"]),
  workflowId: z.string().min(1).optional(),
  branchId: z.string().min(1).optional(),
  revisionId: z.string().min(1).optional(),
  nodeId: z.string().min(1).optional(),
  workspaceId: z.string().min(1).optional(),
  correlationId: z.string().min(1),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  startedAt: z.string().datetime().optional(),
  claimedAt: z.string().datetime().optional(),
  workerId: z.string().min(1).optional(),
  finishedAt: z.string().datetime().optional(),
  retry: z.object({
    attempt: z.number().int().min(0),
    maxAttempts: z.number().int().min(0),
    retryable: z.boolean(),
    nextRunAt: z.string().datetime().optional(),
    backoffSeconds: z.number().min(0).optional()
  }),
  cancelledAt: z.string().datetime().optional(),
  cancellationReason: z.string().min(1).optional(),
  events: z.array(workflowJobEventSchema),
  payload: jsonRecordSchema.optional(),
  result: jsonRecordSchema.optional(),
  error: z.string().min(1).optional()
});

export const workflowGeneratedModuleSignatureSchema = z.object({
  promptHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  inputSchemaHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  outputSchemaHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  runtimeHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  sandboxHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  dependencyManifestHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  replaySeed: z.string().min(1),
  artifactHash: z.string().regex(/^sha256:[a-f0-9]{64}$/)
});

export const workflowGeneratedModuleReuseDecisionSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1),
  nodeId: z.string().min(1),
  status: z.enum(["reuse", "reuse-with-reeval", "regenerate", "blocked-drift"]),
  createdAt: z.string().datetime(),
  sourceBranchId: z.string().min(1).optional(),
  sourceDraftRevisionId: z.string().min(1).optional(),
  sourceEvalReportId: z.string().min(1).optional(),
  signature: workflowGeneratedModuleSignatureSchema,
  gates: z.array(
    z.enum([
      "prompt",
      "schema",
      "runtime",
      "sandbox",
      "dependency",
      "network",
      "replay",
      "evaluation",
      "unresolved-failure"
    ])
  ),
  reason: z.string().min(1),
  artifacts: z.array(workflowArtifactRefSchema)
});

export const workflowWorkspaceSchema = z.object({
  id: z.string().min(1),
  jobId: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  revisionId: z.string().min(1).optional(),
  draftId: z.string().min(1).optional(),
  rootPath: z.string().min(1),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  mountedAgents: z.array(
    z.enum(["workflow-architect", "coder", "tester", "runner", "fixer", "evaluator"])
  ),
  mounts: z.array(
    z.object({
      role: z.enum(["workflow-architect", "coder", "tester", "runner", "fixer", "evaluator"]),
      path: z.string().min(1),
      mode: z.enum(["ro", "rw"])
    })
  ),
  filesCreated: z.array(z.string()),
  fileHashes: z.array(
    z.object({
      path: z.string().min(1),
      checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/)
    })
  ),
  artifactsProduced: z.array(workflowArtifactRefSchema),
  logs: z.array(z.string()),
  logPaths: z.array(z.string()),
  testReports: z.array(z.string()),
  retentionPolicy: z.enum(["ephemeral", "retain-on-failure", "retain"]),
  retentionStatus: z.enum(["active", "retained", "eligible-for-cleanup"])
});

export const workflowDraftEvaluationFindingSchema = z.object({
  id: z.string().min(1),
  severity: z.enum(["info", "warn", "error"]),
  target: z.object({
    kind: z.enum(["workflow", "node", "edge", "artifact"]),
    id: z.string().min(1).optional()
  }),
  message: z.string().min(1),
  issues: z.array(workflowValidationIssueSchema)
});

export const workflowDraftEvaluationSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  draftRevisionId: z.string().min(1),
  jobId: z.string().min(1).optional(),
  status: z.enum(["passed", "failed"]),
  readyForApproval: z.boolean(),
  createdAt: z.string().datetime(),
  finishedAt: z.string().datetime(),
  mode: z.literal("draft"),
  mockOnly: z.literal(true),
  liveProviderCalls: z.literal(0),
  findings: z.array(workflowDraftEvaluationFindingSchema),
  events: z.array(
    z.object({
      id: z.string().min(1),
      timestamp: z.string().datetime(),
      level: z.enum(["info", "warn", "error"]),
      message: z.string().min(1),
      severity: workflowEventSeveritySchema.optional(),
      kind: workflowObservabilityEventKindSchema.optional(),
      workflowId: z.string().min(1).optional(),
      revisionId: z.string().min(1).optional(),
      branchId: z.string().min(1).optional(),
      runId: z.string().min(1).optional(),
      correlationId: z.string().min(1).optional(),
      nodeId: z.string().min(1).optional(),
      metadata: jsonRecordSchema.optional()
    })
  ),
  suggestions: z.array(workflowPlannerSuggestionSchema)
});

export const generatedNodeTestReportSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  nodeId: z.string().min(1),
  jobId: z.string().min(1),
  status: z.enum(["passed", "failed"]),
  createdAt: z.string().datetime(),
  finishedAt: z.string().datetime(),
  testFiles: z.array(workflowArtifactRefSchema),
  resultArtifacts: z.array(workflowArtifactRefSchema),
  logs: z.array(z.string()),
  failureMessage: z.string().min(1).optional()
});

export const generatedNodeEvalReportSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  nodeId: z.string().min(1),
  jobId: z.string().min(1),
  status: z.enum(["passed", "failed"]),
  createdAt: z.string().datetime(),
  finishedAt: z.string().datetime(),
  designSpec: workflowArtifactRefSchema,
  testReportId: z.string().min(1),
  schemaValid: z.boolean(),
  securityValid: z.boolean(),
  replayValid: z.boolean(),
  dependencyPolicyValid: z.boolean(),
  fixHistory: z.array(z.string()),
  findings: z.array(workflowDraftEvaluationFindingSchema)
});

export const workflowDeploymentRecordSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  approvedRevisionId: z.string().min(1),
  draftEvaluationId: z.string().min(1),
  kind: z.enum([
    "schedule.activation",
    "skill.publication",
    "integration.configuration",
    "runner.configuration",
    "workflow.bundle",
    "generated.service"
  ]),
  status: z.enum(["ready", "blocked", "deployed", "rolled-back", "undeployed"]),
  createdAt: z.string().datetime(),
  createdBy: z.string().min(1),
  requiredIntegrations: z.array(z.string()),
  secretRefs: z.array(z.string()),
  rollbackPlan: z.string().min(1),
  auditRecordId: z.string().min(1),
  metadata: jsonRecordSchema
});

export const workflowDeploymentActivationRecordSchema = z.object({
  deploymentId: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  approvedRevisionId: z.string().min(1),
  kind: z.enum([
    "schedule.activation",
    "skill.publication",
    "integration.configuration",
    "runner.configuration",
    "workflow.bundle",
    "generated.service"
  ]),
  status: z.enum(["active", "inactive"]),
  artifactRefs: z.array(workflowArtifactRefSchema),
  runnerConfig: jsonRecordSchema.optional(),
  rollbackTarget: z.string().min(1).optional(),
  activatedAt: z.string().datetime()
});

export const workflowDeploymentRollbackTargetSchema = z.object({
  deploymentId: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  approvedRevisionId: z.string().min(1),
  previousDeploymentId: z.string().min(1).optional(),
  rollbackPlan: z.string().min(1),
  artifactRefs: z.array(workflowArtifactRefSchema),
  createdAt: z.string().datetime()
});

export const workflowRunCheckpointSchema = z.object({
  id: z.string().min(1),
  runId: z.string().min(1),
  workflowId: z.string().min(1),
  approvedRevisionId: z.string().min(1),
  nodeId: z.string().min(1),
  attempt: z.number().int().min(0),
  status: z.enum(["running", "succeeded", "failed", "skipped", "cancelled"]),
  inputHash: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  idempotencyKey: z.string().min(1),
  startedAt: z.string().datetime(),
  finishedAt: z.string().datetime().optional(),
  output: jsonRecordSchema.optional(),
  error: z.string().min(1).optional(),
  workspacePath: z.string().min(1).optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowConnectorAuthRequirementSchema = z.object({
  name: z.string().min(1),
  scheme: z.enum(["none", "apiKey", "bearer", "basic", "oauth"]),
  location: z.enum(["header", "query", "cookie", "body"]).optional(),
  parameterName: z.string().min(1).optional(),
  secretRef: z.string().min(1).optional(),
  description: z.string().min(1).optional()
});

export const workflowConnectorOperationSchema = z.object({
  name: z.string().min(1),
  version: z.string().min(1),
  description: z.string().min(1),
  inputSchema: jsonSchemaShapeSchema,
  outputSchema: jsonSchemaShapeSchema,
  method: z.string().min(1).optional(),
  path: z.string().min(1).optional(),
  toolName: z.string().min(1).optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowConnectorTestResultSchema = z.object({
  status: z.enum(["untested", "succeeded", "failed"]),
  testedAt: z.string().datetime().optional(),
  message: z.string().min(1).optional(),
  operationCount: z.number().int().min(0).optional(),
  metadata: jsonRecordSchema.optional()
});

export const workflowConnectorRecordSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  kind: z.enum(["http", "openapi", "mcp"]),
  adapterId: z.string().min(1),
  sourceUrl: z.string().min(1).optional(),
  endpointUrl: z.string().min(1).optional(),
  transport: z.enum(["streamable-http", "stdio"]).optional(),
  allowedHosts: z.array(z.string().min(1)),
  auth: z.array(workflowConnectorAuthRequirementSchema),
  operations: z.array(workflowConnectorOperationSchema),
  secretRefs: z.record(z.string(), z.string().min(1)),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  lastTest: workflowConnectorTestResultSchema,
  metadata: jsonRecordSchema.optional()
});

export const workflowScheduleRecordSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  deploymentId: z.string().min(1),
  approvedRevisionId: z.string().min(1),
  nodeId: z.string().min(1),
  label: z.string().min(1),
  cron: z.string().min(1),
  timezone: z.string().min(1),
  status: z.enum(["active", "paused", "disabled"]),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  nextFireAt: z.string().datetime(),
  lastFireAt: z.string().datetime().optional(),
  lastRunId: z.string().min(1).optional(),
  lastJobId: z.string().min(1).optional(),
  lastError: z.string().min(1).optional(),
  missedCount: z.number().int().min(0)
});

export const workflowAlertPolicySchema = z.object({
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  enabled: z.boolean(),
  events: z.array(z.enum(["run.failed", "job.failed", "schedule.missed", "deployment.failed"])),
  channels: z.array(z.enum(["email", "telegram", "webhook"])),
  secretRefs: z.record(z.string(), z.string().min(1)),
  updatedAt: z.string().datetime(),
  updatedBy: z.string().min(1)
});

export const workflowRetentionPolicySchema = z.object({
  workflowId: z.string().min(1),
  branchId: z.string().min(1).optional(),
  maxRunEventDays: z.number().int().min(1),
  maxSuccessfulRunWorkspaceDays: z.number().int().min(0),
  maxFailedRunWorkspaceDays: z.number().int().min(0),
  maxJobEventDays: z.number().int().min(1),
  updatedAt: z.string().datetime(),
  updatedBy: z.string().min(1)
});

export const workflowOpsHealthSchema = z.object({
  status: z.enum(["ok", "degraded"]),
  databaseWritable: z.boolean(),
  worker: z.object({
    active: z.boolean(),
    queuedJobs: z.number().int().min(0),
    runningJobs: z.number().int().min(0),
    failedJobs: z.number().int().min(0)
  }),
  scheduler: z.object({
    active: z.boolean(),
    activeSchedules: z.number().int().min(0),
    dueSchedules: z.number().int().min(0)
  }),
  runs: z.object({
    running: z.number().int().min(0),
    resumable: z.number().int().min(0),
    failed: z.number().int().min(0)
  }),
  connectors: z.object({
    total: z.number().int().min(0),
    failedTests: z.number().int().min(0)
  }),
  memory: z.object({
    total: z.number().int().min(0),
    expired: z.number().int().min(0)
  }),
  router: z.object({
    classifierVersion: z.string().min(1),
    evalCases: z.number().int().min(0),
    lastEvalPassed: z.boolean().optional()
  }),
  checkedAt: z.string().datetime()
});

export const workflowAuditExportRecordSchema = z.object({
  id: z.string().min(1),
  workflowId: z.string().min(1),
  exportedAt: z.string().datetime(),
  format: z.literal("jsonl"),
  redacted: z.literal(true),
  lineCount: z.number().int().min(0),
  records: z.array(jsonRecordSchema)
});
