import type { EventEnvelope } from "./events";
import type { SessionsStatusPayload } from "./agents-feed";
import type { FleetHost, FleetSession, FleetStatus } from "./fleet-hosts";

type FetchJSON = <T>(path: string) => Promise<T>;
type FleetCondition = "unreachable" | "stale" | "recovering" | "healthy";

export class FleetHomeView {
  private payload: FleetStatus | undefined;
  private loading = false;
  private status = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly navigateSession: (session: FleetSession) => void,
    private readonly openHost: (hostID: string) => void,
    private readonly openInbox: () => void,
    private readonly headerControl: HTMLElement
  ) {}

  async load(): Promise<void> {
    this.loading = true;
    this.status = "";
    this.render();
    try {
      this.payload = await this.fetchJSON<FleetStatus>("/fleet/status");
    } catch {
      this.payload = undefined;
      this.status = "fleet home unavailable";
    } finally {
      this.loading = false;
      this.render();
    }
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "sessions.status") {
      this.applySessionsStatus(envelope.payload as SessionsStatusPayload);
      return;
    }
    if (
      envelope.type === "approval.requested" ||
      envelope.type === "approval.decided" ||
      envelope.type === "approval.expired" ||
      envelope.type === "session.started" ||
      envelope.type === "session.ended"
    ) {
      void this.load();
    }
  }

  private applySessionsStatus(status: SessionsStatusPayload): void {
    if (this.payload === undefined) {
      return;
    }
    this.payload = {
      ...this.payload,
      sessions: status.sessions.map((session) => ({
        id: session.id,
        host_id: session.host_id,
        agent: session.agent,
        state: session.state,
        last_activity: session.last_activity,
        pending_approvals: session.pending_approvals_count,
        recovery_state: session.recovery_state,
        recovery_reason: session.recovery_reason,
        recovery_updated_at: session.recovery_updated_at,
        remote: session.remote,
        remote_url: session.remote_url
      }))
    };
    this.render();
  }

  private render(): void {
    const shell = document.createElement("section");
    shell.className = "session-list-shell fleet-home-shell";
    const header = document.createElement("div");
    header.className = "session-list-header";
    const title = document.createElement("h1");
    title.textContent = "fleet";
    const reload = document.createElement("button");
    reload.type = "button";
    reload.className = "control-button";
    reload.textContent = "Reload";
    reload.addEventListener("click", () => void this.load());
    header.append(title, this.headerControl, reload);
    shell.append(header, this.body());
    this.root.hidden = false;
    this.root.replaceChildren(shell);
  }

  private body(): HTMLElement {
    if (this.loading) {
      return empty("loading fleet home");
    }
    if (this.payload === undefined) {
      return empty(this.status || "fleet home unavailable");
    }
    const body = document.createElement("div");
    body.className = "fleet-home-grid";
    body.append(this.approvalSummary(), this.hostAttention(), this.activeSessions());
    return body;
  }

  private approvalSummary(): HTMLElement {
    const section = sectionTitle(
      `pending approvals (${this.payload?.pending_approvals.length ?? 0})`
    );
    const open = document.createElement("button");
    open.type = "button";
    open.className = "fleet-home-summary";
    open.textContent =
      (this.payload?.pending_approvals.length ?? 0) === 0
        ? "no pending approvals"
        : "review approval inbox";
    open.addEventListener("click", () => this.openInbox());
    section.append(open);
    return section;
  }

  private hostAttention(): HTMLElement {
    const section = sectionTitle("host attention");
    const hosts = (this.payload?.hosts ?? [])
      .map((host) => ({ host, condition: hostCondition(host, this.sessionsFor(host.id)) }))
      .filter(({ condition }) => condition !== "healthy")
      .sort((left, right) => conditionRank(left.condition) - conditionRank(right.condition));
    if (hosts.length === 0) {
      section.append(empty("no hosts need attention"));
      return section;
    }
    for (const { host, condition } of hosts) {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `fleet-home-row condition-${condition}`;
      row.textContent = `${host.display_name} / ${condition}`;
      row.addEventListener("click", () => this.openHost(host.id));
      section.append(row);
    }
    return section;
  }

  private activeSessions(): HTMLElement {
    const section = sectionTitle("active sessions");
    const sessions = (this.payload?.sessions ?? [])
      .filter((session) => session.state !== "idle")
      .sort((left, right) => sessionRank(left) - sessionRank(right));
    if (sessions.length === 0) {
      section.append(empty("no active sessions"));
      return section;
    }
    for (const session of sessions) {
      const row = document.createElement("button");
      row.type = "button";
      row.className = "fleet-home-row";
      row.textContent = `${session.agent} / ${session.state} / ${session.id}`;
      row.addEventListener("click", () => this.navigateSession(session));
      section.append(row);
    }
    return section;
  }

  private sessionsFor(hostID: string): FleetSession[] {
    return (this.payload?.sessions ?? []).filter((session) => session.host_id === hostID);
  }
}

function hostCondition(host: FleetHost, sessions: FleetSession[]): FleetCondition {
  if (host.state === "stale") {
    return "stale";
  }
  if (host.state !== "active") {
    return "unreachable";
  }
  if (
    sessions.some(
      (session) => session.recovery_state !== undefined && session.recovery_state !== "healthy"
    )
  ) {
    return "recovering";
  }
  return "healthy";
}

function conditionRank(condition: FleetCondition): number {
  return ["unreachable", "stale", "recovering", "healthy"].indexOf(condition);
}

function sessionRank(session: FleetSession): number {
  return ["awaiting-approval", "blocked", "working", "idle"].indexOf(session.state);
}

function sectionTitle(title: string): HTMLElement {
  const section = document.createElement("section");
  section.className = "fleet-home-section";
  const heading = document.createElement("h2");
  heading.textContent = title;
  section.append(heading);
  return section;
}

function empty(message: string): HTMLElement {
  const row = document.createElement("div");
  row.className = "session-list-empty";
  row.textContent = message;
  return row;
}
