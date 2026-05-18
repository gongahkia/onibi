import type { ArtifactManifest, ReplayDecision, ReplayPolicy } from "./types.js";

export function decideReplay(
  previous: ArtifactManifest | undefined,
  next: ArtifactManifest,
  policy: ReplayPolicy
): ReplayDecision {
  if (policy.mode === "always-regenerate") {
    return {
      action: "regenerate",
      reason: "Replay policy requires regeneration."
    };
  }

  if (!previous) {
    return {
      action: "regenerate",
      reason: "No previous artifact manifest is available."
    };
  }

  if (manifestFingerprint(previous) === manifestFingerprint(next)) {
    return {
      action: "reuse",
      reason: "Artifact manifest checksums match."
    };
  }

  if (policy.mode === "fail-on-drift") {
    return {
      action: "fail",
      reason: "Artifact manifest drift detected."
    };
  }

  return {
    action: "regenerate",
    reason: "Artifact manifest drift detected and regeneration is allowed."
  };
}

export function manifestFingerprint(manifest: ArtifactManifest): string {
  return manifest.artifacts
    .map((artifact) => `${artifact.path}:${artifact.checksum}`)
    .sort()
    .join("\n");
}

export const defaultReplayPolicy: ReplayPolicy = {
  mode: "reuse-if-unchanged",
  seed: "kelpclaw-phase-1"
};
