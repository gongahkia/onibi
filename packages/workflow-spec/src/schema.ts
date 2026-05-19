import { z } from "zod";
import { workflowSchemaVersion } from "./types.js";
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
  codegen: workflowCodegenMetadataSchema.optional()
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
  "prompt.planning",
  "skill.matching",
  "draft.edit",
  "node.reprompt",
  "workflow.approval",
  "dag.compilation",
  "node.container",
  "adapter.call",
  "codegen.artifact",
  "delivery.event",
  "run.lifecycle"
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
