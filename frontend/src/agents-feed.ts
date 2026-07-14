import type { EventEnvelope } from "./events";

export type SessionState = "idle" | "working" | "awaiting-approval" | "blocked";
export type SessionRecoveryState =
  "healthy" | "reconnecting" | "recovering" | "orphaned" | "failed" | "terminated";

export type SessionStatus = {
  id: string;
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

type FetchJSON = <T>(path: string) => Promise<T>;

const stateOrder: SessionState[] = ["awaiting-approval", "blocked", "working", "idle"];
const refreshEvents = new Set([
  "session.started",
  "session.ended",
  "session.activity",
  "approval.requested",
  "approval.decided",
  "approval.expired",
  "timeline.entry",
  "cost.updated"
]);

export class AgentsFeed {
  readonly element = document.createElement("div");
  private readonly button = document.createElement("button");
  private readonly label = document.createElement("span");
  private readonly dots = document.createElement("span");
  private readonly menu = document.createElement("div");
  private payload: SessionsStatusPayload | undefined;
  private opened = false;
  private loading = false;
  private refreshTimer = 0;

  constructor(
    private readonly fetchJSON: FetchJSON,
    private readonly navigate: (sessionID: string) => void
  ) {
    this.element.className = "agents-feed";
    this.button.type = "button";
    this.button.className = "control-button agents-feed-button";
    this.button.addEventListener("click", (event) => {
      event.stopPropagation();
      this.toggle();
    });
    this.label.className = "agents-feed-label";
    this.dots.className = "agents-feed-dots";
    this.menu.className = "agents-feed-menu";
    this.menu.hidden = true;
    this.button.append(this.label, this.dots);
    this.element.append(this.button, this.menu);
    document.addEventListener("click", (event) => {
      if (event.target instanceof Node && !this.element.contains(event.target)) {
        this.close();
      }
    });
    this.render();
  }

  async load(): Promise<void> {
    this.loading = true;
    this.render();
    try {
      this.payload = await this.fetchJSON<SessionsStatusPayload>("/sessions/status?include=remote");
    } catch {
      this.payload = emptyPayload();
    } finally {
      this.loading = false;
      this.render();
    }
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "sessions.status") {
      this.payload = envelope.payload as SessionsStatusPayload;
      this.loading = false;
      this.render();
      return;
    }
    if (refreshEvents.has(envelope.type)) {
      this.scheduleRefresh();
    }
  }

  private toggle(): void {
    this.opened = !this.opened;
    this.render();
    if (this.opened && this.payload === undefined) {
      void this.load();
    }
  }

  private close(): void {
    if (!this.opened) {
      return;
    }
    this.opened = false;
    this.render();
  }

  private scheduleRefresh(): void {
    window.clearTimeout(this.refreshTimer);
    this.refreshTimer = window.setTimeout(() => void this.load(), 350);
  }

  private render(): void {
    const payload = this.payload ?? emptyPayload();
    const sessions = payload.sessions ?? [];
    this.label.textContent = this.loading ? "AGT ..." : `AGT ${sessions.length}`;
    this.button.setAttribute("aria-label", feedLabel(payload));
    this.dots.replaceChildren(
      ...stateOrder.map((state) => stateDot(state, payload.counts[state] ?? 0))
    );
    this.menu.hidden = !this.opened;
    if (!this.opened) {
      return;
    }
    const rows = document.createElement("div");
    rows.className = "agents-feed-list";
    if (sessions.length === 0) {
      const empty = document.createElement("div");
      empty.className = "agents-feed-empty";
      empty.textContent = "no sessions";
      rows.append(empty);
    } else {
      for (const session of sortSessions(sessions)) {
        rows.append(this.row(session));
      }
    }
    this.menu.replaceChildren(rows);
  }

  private row(session: SessionStatus): HTMLButtonElement {
    const row = document.createElement("button");
    row.type = "button";
    const attachable = !hasUnhealthyRecovery(session);
    row.className = `agents-feed-row state-${session.state}${attachable ? "" : " recovery-unhealthy"}`;
    row.title = recoveryText(session) ?? session.id;
    row.disabled = !attachable;
    const dot = document.createElement("span");
    dot.className = `agent-dot state-${session.state}`;
    const main = document.createElement("span");
    main.className = "agents-feed-main";
    const title = document.createElement("span");
    title.className = "agents-feed-title";
    title.textContent = sessionTitle(session);
    const meta = document.createElement("span");
    meta.className = "agents-feed-meta";
    const recovery = recoveryText(session);
    meta.textContent = [recovery, stateText(session.state), formatWhen(session.last_activity)]
      .filter((part): part is string => part !== undefined && part !== "")
      .join(" / ");
    main.append(title, meta);
    row.append(dot, main);
    if (attachable) {
      row.addEventListener("click", () => {
        this.close();
        if (session.remote_url !== undefined && session.remote_url !== "") {
          window.location.href = session.remote_url;
          return;
        }
        this.navigate(session.id);
      });
    }
    return row;
  }
}

function emptyPayload(): SessionsStatusPayload {
  return {
    generated_at: "",
    sessions: [],
    counts: {
      idle: 0,
      working: 0,
      "awaiting-approval": 0,
      blocked: 0
    }
  };
}

function stateDot(state: SessionState, count: number): HTMLElement {
  const dot = document.createElement("span");
  dot.className = `agent-dot state-${state}`;
  dot.dataset.count = String(count);
  dot.hidden = count === 0;
  return dot;
}

function feedLabel(payload: SessionsStatusPayload): string {
  const count = payload.sessions.length;
  const parts = stateOrder
    .map((state) => `${payload.counts[state] ?? 0} ${stateText(state)}`)
    .join(", ");
  const recovering = payload.sessions.filter(hasUnhealthyRecovery).length;
  return `${count} sessions: ${parts}${recovering > 0 ? `, ${recovering} recovering` : ""}`;
}

function sortSessions(sessions: SessionStatus[]): SessionStatus[] {
  return [...sessions].sort((a, b) => {
    if (hasUnhealthyRecovery(a) !== hasUnhealthyRecovery(b)) {
      return hasUnhealthyRecovery(a) ? -1 : 1;
    }
    const stateDelta = stateRank(a.state) - stateRank(b.state);
    if (stateDelta !== 0) {
      return stateDelta;
    }
    return sessionTime(b) - sessionTime(a);
  });
}

function stateRank(state: SessionState): number {
  const rank = stateOrder.indexOf(state);
  return rank < 0 ? stateOrder.length : rank;
}

function sessionTitle(session: SessionStatus): string {
  if (session.remote === true) {
    return (session.peer_name ?? session.agent) || shortID(session.id);
  }
  return `${session.agent || "session"} ${shortID(session.id)}`;
}

function sessionTime(session: SessionStatus): number {
  const ts = Date.parse(session.last_activity);
  return Number.isFinite(ts) ? ts : 0;
}

function stateText(state: SessionState): string {
  if (state === "awaiting-approval") {
    return "awaiting";
  }
  return state;
}

function formatWhen(raw: string): string {
  const ts = Date.parse(raw);
  if (!Number.isFinite(ts)) {
    return "-";
  }
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function hasUnhealthyRecovery(session: SessionStatus): boolean {
  return session.recovery_state !== undefined && session.recovery_state !== "healthy";
}

function recoveryText(session: SessionStatus): string | undefined {
  if (!hasUnhealthyRecovery(session)) {
    return undefined;
  }
  const reason = session.recovery_reason?.trim();
  return reason === undefined || reason === ""
    ? `recovery ${session.recovery_state}`
    : `recovery ${session.recovery_state}: ${reason}`;
}

function shortID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}
