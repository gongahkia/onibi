import { jsonRecordSchema } from "@kelpclaw/workflow-spec";
import { z } from "zod";
import { taintSourceSchema } from "./taint.js";

export const blockedUseSchema = z.object({
  kind: z.enum(["tool_call", "agent_plan_step", "agent_instruction", "shell_command"]),
  text: z.string().min(1),
  tool: z.string().min(1).optional(),
  args: jsonRecordSchema.optional()
});

export const firewallPolicyDecisionSchema = z.object({
  action: z.enum(["allow", "require-approval", "deny", "log-only"]),
  matchedRuleIds: z.array(z.string().min(1)),
  reason: z.string().min(1),
  approverRole: z.string().min(1).optional()
});

export const firewallEventSchema = z.object({
  id: z.string().min(1),
  timestamp: z.string().datetime(),
  runId: z.string().min(1),
  eventType: z.literal("tainted_instruction_blocked"),
  source: taintSourceSchema,
  taintedText: z.string().min(1),
  blockedUse: blockedUseSchema,
  policyDecision: firewallPolicyDecisionSchema,
  correctionTask: z.object({
    kind: z.literal("safe_reanalysis"),
    prompt: z.string().min(1)
  })
});

export type BlockedUse = z.infer<typeof blockedUseSchema>;
export type FirewallEvent = z.infer<typeof firewallEventSchema>;
