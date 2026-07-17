export type SessionState =
  "idle" | "working" | "awaiting-approval" | "blocked" | "recovering" | "failed";

export type SessionRecoveryState =
  "healthy" | "reconnecting" | "recovering" | "orphaned" | "failed" | "terminated";

export type SessionStatus = {
  id: string;
  host_id?: string;
  agent: string;
  cwd?: string;
  state: SessionState;
  last_activity: string;
  pending_approvals_count: number;
  recovery_state?: SessionRecoveryState;
  recovery_reason?: string;
  recovery_updated_at?: string;
  role_required: string;
  remote?: boolean;
  peer_name?: string;
  remote_url?: string;
};

export type SessionsStatusPayload = {
  generated_at: string;
  sessions: SessionStatus[];
  counts: Record<SessionState, number>;
};
