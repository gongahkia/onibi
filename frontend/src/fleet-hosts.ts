export type FleetHostState = "pending" | "active" | "stale" | "revoked";
type FleetRecoveryState =
  "healthy" | "reconnecting" | "recovering" | "orphaned" | "failed" | "terminated";
type FleetCondition = "unreachable" | "stale" | "recovering" | "healthy";

export type FleetHost = {
  id: string;
  display_name: string;
  endpoint: { kind: "mesh" | "ssh" | "relay"; url: string };
  protocol_version: number;
  binary_version: string;
  capabilities: string[];
  state: FleetHostState;
  registered_at: string;
  last_seen_at?: string;
  revoked_at?: string;
};

export type FleetSession = {
  id: string;
  host_id?: string;
  agent: string;
  state: "idle" | "working" | "awaiting-approval" | "blocked";
  last_activity: string;
  pending_approvals: number;
  recovery_state?: FleetRecoveryState;
  recovery_reason?: string;
  recovery_updated_at?: string;
  remote?: boolean;
  remote_url?: string;
};

export type FleetApproval = {
  id: string;
  host_id?: string;
  session_id: string;
  agent: string;
  tool: string;
  state: "pending";
  created_at: string;
  expires_at: string;
};

export type FleetStatus = {
  generated_at: string;
  hosts: FleetHost[];
  sessions: FleetSession[];
  pending_approvals: FleetApproval[];
};

type FetchJSON = <T>(path: string) => Promise<T>;

export class FleetHostsPanel {
  readonly element = document.createElement("button");
  private modal: HTMLElement | undefined;
  private payload: FleetStatus | undefined;
  private selectedID = "";
  private loading = false;
  private status = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON
  ) {
    this.element.type = "button";
    this.element.className = "control-button";
    this.element.textContent = "Fleet";
    this.element.addEventListener("click", () => this.open());
  }

  open(): void {
    this.openFor("");
  }

  openHost(id: string): void {
    this.openFor(id);
  }

  private openFor(id: string): void {
    this.selectedID = id;
    this.status = "";
    this.modal?.remove();
    this.modal = document.createElement("div");
    this.modal.className = "share-modal";
    this.modal.addEventListener("click", (event) => {
      if (event.target === this.modal) {
        this.close();
      }
    });
    this.root.append(this.modal);
    void this.load();
  }

  private close(): void {
    this.modal?.remove();
    this.modal = undefined;
  }

  private async load(): Promise<void> {
    this.loading = true;
    this.status = "";
    this.render();
    try {
      this.payload = await this.fetchJSON<FleetStatus>("/fleet/status");
      if (!this.payload.hosts.some((host) => host.id === this.selectedID)) {
        this.selectedID = "";
      }
    } catch {
      this.payload = undefined;
      this.status = "fleet status unavailable";
    } finally {
      this.loading = false;
      this.render();
    }
  }

  private render(): void {
    if (this.modal === undefined) {
      return;
    }
    const shell = document.createElement("section");
    shell.className = "share-form fleet-hosts-form";
    const header = document.createElement("div");
    header.className = "share-header";
    const title = document.createElement("div");
    title.className = "share-title";
    title.textContent = this.selectedHost()?.display_name ?? "fleet hosts";
    header.append(title);
    if (this.selectedID !== "") {
      const back = button("Back");
      back.addEventListener("click", () => {
        this.selectedID = "";
        this.render();
      });
      header.append(back);
    }
    const reload = button("Reload");
    reload.addEventListener("click", () => void this.load());
    const close = button("Close");
    close.addEventListener("click", () => this.close());
    header.append(reload, close);
    shell.append(header, this.body());
    this.modal.replaceChildren(shell);
  }

  private body(): HTMLElement {
    if (this.loading) {
      return empty("loading fleet hosts");
    }
    if (this.payload === undefined) {
      return empty(this.status || "fleet status unavailable");
    }
    const host = this.selectedHost();
    return host === undefined ? this.hostList() : this.hostDetail(host);
  }

  private hostList(): HTMLElement {
    const list = document.createElement("div");
    list.className = "fleet-host-list";
    if (this.payload?.hosts.length === 0) {
      list.append(empty("no enrolled hosts"));
      return list;
    }
    for (const host of sortedHosts(this.payload?.hosts ?? [])) {
      const sessions = this.sessionsFor(host.id);
      const condition = hostCondition(host, sessions);
      const row = document.createElement("button");
      row.type = "button";
      row.className = `fleet-host-row condition-${condition}`;
      row.setAttribute("aria-label", `${host.display_name}, ${condition}`);
      const name = document.createElement("span");
      name.className = "fleet-host-name";
      name.textContent = host.display_name;
      const state = document.createElement("span");
      state.className = "fleet-host-condition";
      state.textContent = condition;
      const meta = document.createElement("span");
      meta.className = "fleet-host-meta";
      meta.textContent = `${host.endpoint.kind} / ${sessions.length} session${sessions.length === 1 ? "" : "s"}`;
      row.append(name, state, meta);
      row.addEventListener("click", () => {
        this.selectedID = host.id;
        this.render();
      });
      list.append(row);
    }
    return list;
  }

  private hostDetail(host: FleetHost): HTMLElement {
    const sessions = this.sessionsFor(host.id);
    const detail = document.createElement("div");
    detail.className = "fleet-host-detail";
    const condition = hostCondition(host, sessions);
    detail.append(
      detailSection("operational state", [
        detailRow("health", condition, `condition-${condition}`),
        detailRow("host state", host.state),
        detailRow("last seen", host.last_seen_at || "no heartbeat reported")
      ]),
      detailSection("transport", [
        detailRow("kind", host.endpoint.kind),
        detailRow("endpoint", host.endpoint.url)
      ]),
      detailSection("host", [
        detailRow("id", host.id),
        detailRow("binary", host.binary_version),
        detailRow("protocol", String(host.protocol_version)),
        detailRow(
          "capabilities",
          host.capabilities.length === 0 ? "none reported" : host.capabilities.join(", ")
        )
      ]),
      this.sessionsSection(sessions),
      this.approvalsSection(sessions)
    );
    return detail;
  }

  private sessionsSection(sessions: FleetSession[]): HTMLElement {
    const rows =
      sessions.length === 0
        ? [detailRow("sessions", "no reported sessions")]
        : sessions.map((session) =>
            detailRow(
              `${session.agent} / ${session.state}`,
              [sessionRecoveryText(session), session.last_activity]
                .filter((value) => value !== "")
                .join(" / ")
            )
          );
    return detailSection(`sessions (${sessions.length})`, rows);
  }

  private approvalsSection(sessions: FleetSession[]): HTMLElement {
    const ids = new Set(sessions.map((session) => session.id));
    const approvals = (this.payload?.pending_approvals ?? []).filter((approval) =>
      ids.has(approval.session_id)
    );
    const rows =
      approvals.length === 0
        ? [detailRow("pending approvals", "none")]
        : approvals.map((approval) =>
            detailRow(
              `${approval.agent} / ${approval.tool}`,
              `pending / expires ${approval.expires_at}`
            )
          );
    return detailSection(`pending approvals (${approvals.length})`, rows);
  }

  private selectedHost(): FleetHost | undefined {
    return this.payload?.hosts.find((host) => host.id === this.selectedID);
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

function sessionRecoveryText(session: FleetSession): string {
  if (session.recovery_state === undefined || session.recovery_state === "") {
    return "recovery not reported";
  }
  if (session.recovery_state === "healthy") {
    return "recovery healthy";
  }
  return `recovery ${session.recovery_state}${session.recovery_reason ? `: ${session.recovery_reason}` : ""}`;
}

function sortedHosts(hosts: FleetHost[]): FleetHost[] {
  return [...hosts].sort(
    (left, right) =>
      left.display_name.localeCompare(right.display_name) || left.id.localeCompare(right.id)
  );
}

function detailSection(title: string, rows: HTMLElement[]): HTMLElement {
  const section = document.createElement("section");
  section.className = "fleet-host-section";
  const heading = document.createElement("h2");
  heading.textContent = title;
  section.append(heading, ...rows);
  return section;
}

function detailRow(label: string, value: string, className = ""): HTMLElement {
  const row = document.createElement("div");
  row.className = "fleet-host-detail-row";
  const key = document.createElement("span");
  key.className = "fleet-host-detail-key";
  key.textContent = label;
  const content = document.createElement("span");
  content.className = `fleet-host-detail-value ${className}`.trim();
  content.textContent = value;
  row.append(key, content);
  return row;
}

function empty(message: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-empty";
  el.textContent = message;
  return el;
}

function button(label: string): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "approval-button secondary";
  el.textContent = label;
  return el;
}
