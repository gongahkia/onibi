import type { TerminalInlineImageMode } from "./sessions";

export type TerminalRenderProfileReason = "request" | "dispose";

export interface TerminalRenderProfileReport {
  ptyId: string;
  renderer: "webgl" | "dom";
  inlineImageMode: TerminalInlineImageMode;
  rows: number;
  cols: number;
  bytes: number;
  chunks: number;
  batches: number;
  maxBatchBytes: number;
  avgFlushLatencyMs: number;
  maxFlushLatencyMs: number;
  replayBytes: number;
  replayDurationMs: number | null;
  totalDurationMs: number;
  capturedAt: number;
  reason: TerminalRenderProfileReason;
}

const MAX_RECENT_PROFILES = 20;
const recentProfiles: TerminalRenderProfileReport[] = [];

export function terminalRenderProfilingEnabled(): boolean {
  try {
    return globalThis.localStorage?.getItem("onibiTerminalDebug") === "1";
  } catch {
    return false;
  }
}

export function recordTerminalRenderProfile(report: TerminalRenderProfileReport): void {
  recentProfiles.unshift(report);
  recentProfiles.splice(MAX_RECENT_PROFILES);
  window.dispatchEvent(
    new CustomEvent("onibi:terminal-render-profile", { detail: report }),
  );
}

export function getRecentTerminalRenderProfiles(): TerminalRenderProfileReport[] {
  return recentProfiles.slice();
}

export function clearTerminalRenderProfiles(): void {
  recentProfiles.length = 0;
}

export function latestTerminalRenderProfile(
  ptyId?: string | null,
): TerminalRenderProfileReport | null {
  return recentProfiles.find((profile) => !ptyId || profile.ptyId === ptyId) ?? null;
}

export function requestTerminalRenderProfile(
  ptyId?: string | null,
): TerminalRenderProfileReport | null {
  window.dispatchEvent(
    new CustomEvent("onibi:terminal-render-profile-request", {
      detail: ptyId ? { ptyId } : {},
    }),
  );
  return latestTerminalRenderProfile(ptyId);
}

export async function copyTerminalRenderProfile(
  ptyId?: string | null,
): Promise<TerminalRenderProfileReport | null> {
  const report = requestTerminalRenderProfile(ptyId);
  if (!report) {
    return null;
  }
  await navigator.clipboard?.writeText(JSON.stringify(report, null, 2));
  return report;
}
