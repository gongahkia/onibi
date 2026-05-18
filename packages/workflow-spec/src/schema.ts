import { z } from "zod";
import type { JsonValue } from "./types.js";

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

export const workflowNodeSchema = z.object({
  id: z.string().min(1),
  type: z.enum(["skill", "adapter", "codegen", "approval"]),
  label: z.string().min(1),
  skillId: z.string().min(1).optional(),
  adapterId: z.string().min(1).optional(),
  docker: z
    .object({
      image: z.string().min(1),
      command: z.array(z.string().min(1)).min(1),
      env: z.record(z.string(), z.string()).optional()
    })
    .optional(),
  inputs: z.record(z.string(), jsonValueSchema).optional(),
  outputs: z.array(z.string().min(1)).optional()
});

export const workflowEdgeSchema = z.object({
  id: z.string().min(1).optional(),
  source: z.string().min(1),
  target: z.string().min(1)
});

export const workflowApprovalGateSchema = z.object({
  id: z.string().min(1),
  nodeId: z.string().min(1),
  label: z.string().min(1),
  requiredRole: z.enum(["operator", "owner"])
});

export const workflowMetadataSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  version: z.string().min(1),
  createdAt: z.string().datetime().optional()
});

export const workflowSpecSchema = z.object({
  metadata: workflowMetadataSchema,
  nodes: z.array(workflowNodeSchema).min(1),
  edges: z.array(workflowEdgeSchema),
  approvals: z.array(workflowApprovalGateSchema).optional()
});
