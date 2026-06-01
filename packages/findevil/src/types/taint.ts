import { z } from "zod";

export const sensitivityLabelSchema = z.enum([
  "case-data",
  "credential",
  "personal-data",
  "malware",
  "unknown"
]);

export const taintSourceSchema = z.object({
  kind: z.enum(["case_artifact", "tool_output", "agent_trace"]),
  path: z.string().min(1),
  sha256: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  locator: z.string().min(1)
});

export const taintLedgerEntrySchema = z.object({
  id: z.string().min(1),
  runId: z.string().min(1).optional(),
  source: taintSourceSchema,
  text: z.string().min(1),
  extractionTool: z.string().min(1),
  extractedAt: z.string().datetime(),
  sensitivity: sensitivityLabelSchema,
  span: z
    .object({
      start: z.number().int().min(0),
      end: z.number().int().min(0)
    })
    .optional()
});

export type SensitivityLabel = z.infer<typeof sensitivityLabelSchema>;
export type TaintSource = z.infer<typeof taintSourceSchema>;
export type TaintLedgerEntry = z.infer<typeof taintLedgerEntrySchema>;
