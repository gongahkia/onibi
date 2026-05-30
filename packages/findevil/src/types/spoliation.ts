import { z } from "zod";

export const evidenceFileHashSchema = z.object({
  path: z.string().min(1),
  sha256: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  sizeBytes: z.number().int().min(0)
});

export const spoliationCheckSchema = z.object({
  id: z.string().min(1),
  root: z.string().min(1),
  checkedAt: z.string().datetime(),
  ok: z.boolean(),
  before: z.array(evidenceFileHashSchema),
  after: z.array(evidenceFileHashSchema),
  added: z.array(z.string().min(1)),
  removed: z.array(z.string().min(1)),
  changed: z.array(z.string().min(1))
});

export type EvidenceFileHash = z.infer<typeof evidenceFileHashSchema>;
export type SpoliationCheck = z.infer<typeof spoliationCheckSchema>;

// TODO: phase 2B replace placeholder hash rows with evidence-tree hashing.
export const emptySpoliationCheck: SpoliationCheck = spoliationCheckSchema.parse({
  id: "spoliation-check-placeholder",
  root: ".",
  checkedAt: "1970-01-01T00:00:00.000Z",
  ok: true,
  before: [],
  after: [],
  added: [],
  removed: [],
  changed: []
});
