import { invoke } from "@tauri-apps/api/core";

export interface AgentReviewRecord {
  id: string;
  sessionId: string;
  path: string;
  fullPath: string;
  status: "added" | "modified" | "deleted";
  recordedAt: number;
}

export interface AgentReviewDiff {
  id: string;
  path: string;
  oldLabel: string;
  newLabel: string;
  oldText?: string | null;
  newText?: string | null;
  binary: boolean;
}

export function startAgentReview(sessionId: string, root: string): Promise<void> {
  return invoke("agent_review_start", { sessionId, root });
}

export function stopAgentReview(sessionId: string): Promise<void> {
  return invoke("agent_review_stop", { sessionId });
}

export function listAgentReviews(root: string): Promise<AgentReviewRecord[]> {
  return invoke<AgentReviewRecord[]>("agent_review_records", { root });
}

export function getAgentReviewDiff(
  root: string,
  path: string,
): Promise<AgentReviewDiff> {
  return invoke<AgentReviewDiff>("agent_review_diff", { root, path });
}

export function acceptAgentReview(root: string, path: string): Promise<void> {
  return invoke("agent_review_accept", { root, path });
}

export function rejectAgentReview(root: string, path: string): Promise<void> {
  return invoke("agent_review_reject", { root, path });
}
