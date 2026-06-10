import type {
  Approval,
  PaneTarget,
  PendingRemoteDispatch,
  RunEvent,
} from "./types";

export function buildPaneTargetOptions(
  paneTargets: PaneTarget[],
  recent: RunEvent[],
  terminalOutput: Record<string, string[]>,
  pending: Approval[],
): PaneTarget[] {
  const targets = new Map<string, PaneTarget>();
  for (const target of paneTargets) {
    targets.set(target.paneId, target);
  }
  for (const approval of pending) {
    if (!approval.session_id || targets.has(approval.session_id)) {
      continue;
    }
    targets.set(approval.session_id, {
      paneId: approval.session_id,
      sessionId: approval.session_id,
      label: `${approval.agent} ${approval.tool}`,
      agent: approval.agent,
      cwd: approval.cwd,
      status: "blocked",
      trustMode: "approval-required",
    });
  }
  for (const event of recent) {
    if (!event.session_id || targets.has(event.session_id)) {
      continue;
    }
    targets.set(event.session_id, {
      paneId: event.session_id,
      sessionId: event.session_id,
      label: event.session_id,
      status: event.kind,
      trustMode: "approval-required",
    });
  }
  for (const sessionId of Object.keys(terminalOutput).sort()) {
    if (targets.has(sessionId)) {
      continue;
    }
    targets.set(sessionId, {
      paneId: sessionId,
      sessionId,
      label: sessionId,
      status: "mirrored",
      trustMode: "approval-required",
    });
  }
  return [...targets.values()];
}

export function resolveRemoteTarget(
  explicitTarget: string,
  targets: PaneTarget[],
  recent: RunEvent[],
  pending: Approval[],
  targetApprovalId: string | null,
): string {
  if (explicitTarget && targets.some((target) => target.paneId === explicitTarget)) {
    return explicitTarget;
  }
  const approvalTarget = pending.find(
    (approval) => approval.approval_id === targetApprovalId && approval.session_id,
  )?.session_id;
  if (approvalTarget && targets.some((target) => target.paneId === approvalTarget)) {
    return approvalTarget;
  }
  const recentTarget = recent.find((event) =>
    targets.some((target) => target.paneId === event.session_id),
  )?.session_id;
  if (recentTarget) {
    return recentTarget;
  }
  return targets.length === 1 ? targets[0].paneId : "";
}

export function needsRemoteConfirmation(
  target: PaneTarget | undefined,
  destructive: boolean,
): boolean {
  return Boolean(target && (destructive || target.trustMode === "approval-required"));
}

export function sendRemoteTextRequest(text: string, sendEnter: boolean, confirmed: boolean) {
  return {
    text,
    sendEnter,
    confirmed,
  };
}

export function sendRemotePresetRequest(preset: string, confirmed: boolean) {
  return {
    preset,
    confirmed,
  };
}

export function confirmationMessageForDispatch(
  target: PaneTarget | undefined,
  dispatch: PendingRemoteDispatch,
): string {
  if (dispatch.destructive) {
    return `${dispatch.message} This preset can interrupt the running process.`;
  }
  if (target?.trustMode === "approval-required") {
    return `${dispatch.message} This session requires approval confirmation.`;
  }
  return dispatch.message;
}

export function emergencyStopRequest(): RequestInit {
  return {
    method: "POST",
    body: "{}",
  };
}
