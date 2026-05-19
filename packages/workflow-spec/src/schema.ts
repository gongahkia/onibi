import { z } from "zod";
import { workflowSchemaVersion, workflowValidationErrorCodes } from "./types.js";
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

export const workflowAgentBudgetSchema = z.object({
  maxIterations: z.number().int().min(0),
  maxWallClockSeconds: z.number().int().positive(),
  maxModelCostUsd: z.number().min(0),
  maxDockerRuntimeSeconds: z.number().int().min(0),
  maxRetries: z.number().int().min(0)
});

export const workflowAgenticNodePolicySchema = z.object({
  tools: z.array(z.string().min(1)),
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

export const workflowNodeSchema = z.object({
  id: z.string().min(1),
  kind: z.enum(["trigger", "skill", "codegen", "transform", "approval", "delivery"]),
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
  agentic: workflowAgenticNodePolicySchema.optional()
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
  "workspace.artifact",
  "dag.compilation",
  "node.container",
  "adapter.call",
  "codegen.artifact",
  "delivery.event",
  "run.lifecycle",
  "deployment.lifecycle"
]);

export const workflowObservabilityContextSchema = z.object({
  workflowId: z.string().min(1),
  revisionId: z.string().min(1),
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
  "workflow.approved",
  "codegen.reviewed",
  "task.routed",
  "planner.feedback.created",
  "draft.evaluated",
  "job.created",
  "agent.ran",
  "workspace.created",
  "deployment.created",
  "secret.referenced",
  "container.ran",
  "adapter.called",
  "delivery.completed",
  "run.completed"
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
  createdAt: z.string().datetime()
});

export const workflowTaskRouteSchema = z.object({
  route: z.enum(["deterministic", "adapter", "codegen", "agentic", "deployment"]),
  rationale: z.string().min(1),
  requiredModel: workflowModelRequirementSchema,
  expectedNodeKinds: z.array(z.enum(["trigger", "skill", "codegen", "transform", "approval", "delivery"])),
  dockerSandboxRequired: z.boolean(),
  draftTestsRequired: z.boolean(),
  productionDeterministic: z.boolean(),
  modelInvocations: z.array(workflowModelInvocationRecordSchema)
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
  graphDiffId: z.string().min(1),
  route: workflowTaskRouteSchema,
  createdAt: z.string().datetime(),
  status: z.enum(["ready", "warnings", "blocked"]),
  suggestions: z.array(workflowPlannerSuggestionSchema),
  issues: z.array(workflowValidationIssueSchema)
});

export const workflowJobEventSchema = z.object({
  id: z.string().min(1),
  jobId: z.string().min(1),
  timestamp: z.string().datetime(),
  level: z.enum(["info", "error"]),
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
  revisionId: z.string().min(1).optional(),
  nodeId: z.string().min(1).optional(),
  workspaceId: z.string().min(1).optional(),
  correlationId: z.string().min(1),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  startedAt: z.string().datetime().optional(),
  finishedAt: z.string().datetime().optional(),
  retry: z.object({
    attempt: z.number().int().min(0),
    maxAttempts: z.number().int().min(0),
    retryable: z.boolean()
  }),
  cancelledAt: z.string().datetime().optional(),
  cancellationReason: z.string().min(1).optional(),
  events: z.array(workflowJobEventSchema),
  result: jsonRecordSchema.optional(),
  error: z.string().min(1).optional()
});

const workflowArtifactRefSchema = z.object({
  path: z.string().min(1),
  checksum: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  contentType: z.enum(["application/json", "text/markdown", "text/plain", "text/typescript"])
});

export const workflowWorkspaceSchema = z.object({
  id: z.string().min(1),
  jobId: z.string().min(1),
  workflowId: z.string().min(1),
  revisionId: z.string().min(1).optional(),
  draftId: z.string().min(1).optional(),
  createdAt: z.string().datetime(),
  updatedAt: z.string().datetime(),
  mountedAgents: z.array(z.enum(["planner", "coder", "tester", "runner", "fixer"])),
  filesCreated: z.array(z.string()),
  artifactsProduced: z.array(workflowArtifactRefSchema),
  logs: z.array(z.string()),
  testReports: z.array(z.string()),
  retentionPolicy: z.enum(["ephemeral", "retain-on-failure", "retain"])
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
      level: z.enum(["info", "error"]),
      message: z.string().min(1),
      severity: workflowEventSeveritySchema.optional(),
      kind: workflowObservabilityEventKindSchema.optional(),
      workflowId: z.string().min(1).optional(),
      revisionId: z.string().min(1).optional(),
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
  status: z.enum(["ready", "blocked", "deployed", "rolled-back"]),
  createdAt: z.string().datetime(),
  createdBy: z.string().min(1),
  requiredIntegrations: z.array(z.string()),
  secretRefs: z.array(z.string()),
  rollbackPlan: z.string().min(1),
  auditRecordId: z.string().min(1),
  metadata: jsonRecordSchema
});
