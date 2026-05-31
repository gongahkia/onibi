import { mkdir, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { resolveAttackTechniquesForClaim } from "./index.js";
import type { ClaimLedger } from "../types/claim.js";

export type NavigatorDomain = "enterprise-attack" | "mobile-attack" | "ics-attack";

export interface NavigatorLayerV45 {
  readonly name: string;
  readonly versions: NavigatorLayerVersionsV45;
  readonly domain: NavigatorDomain;
  readonly description: string;
  readonly techniques: readonly NavigatorTechniqueV45[];
  readonly gradient: NavigatorGradient;
}

export interface NavigatorLayerVersionsV45 {
  readonly attack?: string | undefined;
  readonly navigator: string;
  readonly layer: "4.5";
}

export interface NavigatorTechniqueV45 {
  readonly techniqueID: string;
  readonly score: number;
  readonly comment: string;
  readonly color: string;
}

export interface NavigatorGradient {
  readonly colors: readonly string[];
  readonly minValue: number;
  readonly maxValue: number;
}

export interface BuildNavigatorLayerOptions {
  readonly name?: string | undefined;
  readonly description?: string | undefined;
  readonly domain?: NavigatorDomain | undefined;
  readonly attackVersion?: string | undefined;
  readonly navigatorVersion?: string | undefined;
}

interface TechniqueCoverage {
  readonly techniqueID: string;
  readonly claimIds: string[];
  confirmed: number;
  total: number;
}

const lowCoverageColor = "#d73027";
const techniqueColorGradient = [
  lowCoverageColor,
  "#fc8d59",
  "#fee08b",
  "#91cf60",
  "#1a9850"
] as const;

export function buildNavigatorLayer(
  ledger: ClaimLedger,
  opts: BuildNavigatorLayerOptions = {}
): NavigatorLayerV45 {
  const navigatorVersion = opts.navigatorVersion ?? "5.2.0";
  const versions: NavigatorLayerVersionsV45 =
    opts.attackVersion === undefined
      ? { navigator: navigatorVersion, layer: "4.5" }
      : { attack: opts.attackVersion, navigator: navigatorVersion, layer: "4.5" };

  return {
    name: opts.name ?? `${ledger.id} ATT&CK coverage`,
    versions,
    domain: opts.domain ?? "enterprise-attack",
    description: opts.description ?? `Technique coverage for claim ledger ${ledger.id}.`,
    techniques: techniqueCoverage(ledger),
    gradient: {
      colors: techniqueColorGradient,
      minValue: 0,
      maxValue: 1
    }
  };
}

export async function writeNavigatorLayer(path: string, layer: NavigatorLayerV45): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(layer, null, 2)}\n`, "utf8");
}

function techniqueCoverage(ledger: ClaimLedger): NavigatorTechniqueV45[] {
  const coverageByTechnique = new Map<string, TechniqueCoverage>();
  for (const claim of ledger.claims) {
    for (const technique of resolveAttackTechniquesForClaim(claim)) {
      const coverage = coverageByTechnique.get(technique.id) ?? {
        techniqueID: technique.id,
        claimIds: [],
        confirmed: 0,
        total: 0
      };
      coverage.claimIds.push(claim.id);
      coverage.total += 1;
      if (claim.status === "confirmed") {
        coverage.confirmed += 1;
      }
      coverageByTechnique.set(technique.id, coverage);
    }
  }

  return [...coverageByTechnique.values()]
    .sort((left, right) => left.techniqueID.localeCompare(right.techniqueID))
    .map((coverage) => {
      const score = coverage.total === 0 ? 0 : coverage.confirmed / coverage.total;
      return {
        techniqueID: coverage.techniqueID,
        score,
        comment: `Claim IDs: ${coverage.claimIds.sort((left, right) => left.localeCompare(right)).join(", ")}`,
        color: colorForScore(score)
      };
    });
}

function colorForScore(score: number): string {
  const boundedScore = Math.max(0, Math.min(1, score));
  const index = Math.round(boundedScore * (techniqueColorGradient.length - 1));
  return techniqueColorGradient[index] ?? lowCoverageColor;
}
