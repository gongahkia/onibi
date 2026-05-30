export interface SentinelOptions {
  readonly casePath?: string | undefined;
  readonly outDir?: string | undefined;
  readonly maxIterations?: number | undefined;
}

export interface SentinelResult {
  readonly ok: boolean;
  readonly status: "not_implemented";
  readonly outputs: readonly string[];
}

// TODO: phase 2D orchestrate claim verification, spoliation checks, and taint firewall.
export function runSentinel(opts: SentinelOptions = {}): SentinelResult {
  void opts;
  return {
    ok: false,
    status: "not_implemented",
    outputs: []
  };
}
