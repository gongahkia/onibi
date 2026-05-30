import {
  emptySpoliationCheck,
  type EvidenceFileHash,
  type SpoliationCheck
} from "../types/spoliation.js";

// TODO: phase 2B hash evidence files recursively with stable path ordering.
export async function hashEvidenceTree(root: string): Promise<readonly EvidenceFileHash[]> {
  void root;
  return [];
}

// TODO: phase 2B compare before/after evidence hashes and report spoliation.
export function spoliationCheck(
  before: readonly EvidenceFileHash[],
  after: readonly EvidenceFileHash[]
): SpoliationCheck {
  return {
    ...emptySpoliationCheck,
    before: [...before],
    after: [...after]
  };
}
