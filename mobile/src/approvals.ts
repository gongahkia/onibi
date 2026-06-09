import type { Approval, Decision } from "./types";
import { isRecord } from "./utils";

export function commandText(input: unknown): string {
  if (isRecord(input) && typeof input.command === "string") {
    return input.command;
  }
  return JSON.stringify(input, null, 2);
}

export function editedInput(input: unknown, editedCommand: string): unknown {
  if (isRecord(input) && typeof input.command === "string") {
    return { ...input, command: editedCommand };
  }
  try {
    return JSON.parse(editedCommand);
  } catch {
    return editedCommand;
  }
}

export function approvalSupportsUpdatedInput(approval: Approval): boolean {
  if (!isRecord(approval.metadata)) {
    return true;
  }
  return approval.metadata.supportsUpdatedInput !== false;
}

export function approvalRiskBadges(approval: Approval, text: string): string[] {
  const lower = text.toLowerCase();
  const badges = new Set<string>();
  if (/\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)\b/.test(lower) || /\brm\s+-rf\b/.test(lower)) {
    badges.add("Destructive delete");
  }
  if (/\bsudo\b/.test(lower)) {
    badges.add("Elevated command");
  }
  if (/\b(curl|wget)\b.*\|\s*(sh|bash|zsh)\b/.test(lower)) {
    badges.add("Network script");
  }
  if (approval.cwd && /(\s|^)(\/|~\/|\.\.\/)/.test(text) && !text.includes(approval.cwd)) {
    badges.add("Outside cwd");
  }
  return [...badges];
}

export function swipeDecision(deltaX: number, width: number): Decision | null {
  const threshold = Math.max(96, width * 0.28);
  if (deltaX >= threshold) {
    return "allow";
  }
  if (deltaX <= -threshold) {
    return "deny";
  }
  return null;
}

export function buildDecisionBody(
  approval: Approval,
  decision: Decision,
  editedCommand?: string,
  reason?: string,
) {
  const body: {
    decision: Decision;
    by: string;
    reason?: string;
    updatedInput?: unknown;
  } = { decision, by: "mobile" };
  if (decision === "deny") {
    body.reason = reason?.trim() || "denied from mobile";
  }
  if (
    decision === "allow" &&
    editedCommand !== undefined &&
    approvalSupportsUpdatedInput(approval)
  ) {
    body.updatedInput = editedInput(approval.input, editedCommand);
    body.reason = "edited from mobile";
  }
  return body;
}
