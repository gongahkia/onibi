import type { Claim, ClaimLedger } from "../types/claim.js";

export interface ExpectedFinding {
  readonly id: string;
  readonly claimId: string;
  readonly type?: string | undefined;
  readonly description?: string | undefined;
  readonly acceptedTechniques: readonly string[];
}

export interface GroundTruthMatch {
  readonly expectedFindingId: string;
  readonly expectedClaimId: string;
  readonly claimId?: string | undefined;
  readonly claimStatus?: Claim["status"] | undefined;
  readonly acceptedTechniqueIds: readonly string[];
  readonly claimTechniqueIds: readonly string[];
  readonly matchedTechniqueIds: readonly string[];
  readonly truePositive: boolean;
  readonly falsePositive: boolean;
  readonly falseNegative: boolean;
}

export interface BenchmarkScore {
  readonly confirmedClaims: number;
  readonly truePositives: number;
  readonly falsePositives: number;
  readonly falseNegatives: number;
  readonly hallucinationCount: number;
  readonly hallucinationRate: number;
  readonly precision: number;
  readonly recall: number;
  readonly f1: number;
}

export interface BenchmarkReport extends BenchmarkScore {
  readonly hallucinationDefinition: string;
  readonly expectedFindings: number;
  readonly evaluatedClaims: number;
  readonly matches: readonly GroundTruthMatch[];
  readonly unmatchedFalsePositiveClaims: readonly GroundTruthMatch[];
}

export type BenchmarkCaseManifest =
  | string
  | readonly ExpectedFinding[]
  | {
      readonly expectedFindings?: readonly unknown[] | undefined;
    };

export type BenchmarkLedger = Pick<ClaimLedger, "claims">;
