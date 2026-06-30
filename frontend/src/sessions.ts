import type { EventEnvelope } from "./events";

const lastSessionKey = "onibi-last-session-id";

export type SessionSummary = {
  id: string;
  agent: string;
  cwd: string;
  started_at: string;
  last_activity: string;
  pending_approvals_count: number;
  tokens_used: number;
  cost_usd: number;
  role_required: string;
  remote?: boolean;
  peer_name?: string;
  remote_url?: string;
};

export type SessionCostPayload = {
  session_id: string;
  model?: string;
  input_tokens: number;
  output_tokens: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  daily_tokens: number;
  cost_known: boolean;
  total_micro_cents?: number;
  total_usd?: number;
  updated_at?: string;
};

type FetchJSON = <T>(path: string) => Promise<T>;

export class SessionsListView {
  private rows: SessionSummary[] = [];
  private loading = false;
  private status = "";
  private workspace = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly navigate: (sessionID: string) => void,
    private readonly workspaceControl?: HTMLElement
  ) {}

  async load(): Promise<void> {
    this.loading = true;
    this.status = "";
    this.render();
    try {
      this.rows = await this.fetchJSON<SessionSummary[]>(this.sessionsPath());
    } catch {
      this.status = "sessions unavailable";
    } finally {
      this.loading = false;
      this.render();
    }
  }

  setWorkspace(name: string, reload = true): void {
    this.workspace = name.trim();
    this.rows = [];
    if (reload) {
      void this.load();
    } else {
      this.render();
    }
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "session.started") {
      const payload = envelope.payload as { session_id?: string };
      if (payload.session_id !== undefined && payload.session_id !== "" && !this.rows.some((row) => row.id === payload.session_id)) {
        void this.load();
      }
      return;
    }
    if (envelope.type !== "cost.updated") {
      return;
    }
    const payload = envelope.payload as { session_id?: string };
    if (payload.session_id === undefined || payload.session_id === "") {
      return;
    }
    void this.loadCost(payload.session_id);
  }

  private sessionsPath(): string {
    const params = new URLSearchParams();
    params.set("include", this.workspace === "" ? "remote" : "local");
    if (this.workspace !== "") {
      params.set("workspace", this.workspace);
    }
    return `/sessions?${params.toString()}`;
  }

  private async loadCost(id: string): Promise<void> {
    try {
      const cost = await this.fetchJSON<SessionCostPayload>(`/sessions/${encodeURIComponent(id)}/cost`);
      const index = this.rows.findIndex((row) => row.id === id);
      if (index < 0) {
        await this.load();
        return;
      }
      const rows = [...this.rows];
      rows[index] = {
        ...rows[index],
        tokens_used: cost.total_tokens,
        cost_usd: cost.cost_known && cost.total_usd !== undefined ? cost.total_usd : 0,
        last_activity: cost.updated_at ?? rows[index].last_activity
      };
      this.rows = rows;
      this.render();
    } catch {
      return;
    }
  }

  private render(): void {
    const shell = document.createElement("section");
    shell.className = "session-list-shell";
    const header = document.createElement("div");
    header.className = "session-list-header";
    const title = document.createElement("h1");
    title.textContent = "sessions";
    const reload = document.createElement("button");
    reload.type = "button";
    reload.className = "control-button";
    reload.textContent = "Reload";
    reload.addEventListener("click", () => void this.load());
    header.append(title);
    if (this.workspaceControl !== undefined) {
      header.append(this.workspaceControl);
    }
    header.append(reload);

    const body = document.createElement("div");
    body.className = "session-list-grid";
    if (this.loading) {
      body.append(emptyRow("loading"));
    } else if (this.rows.length === 0) {
      body.append(emptyRow(this.status || "no active sessions"));
    } else {
      for (const row of sortedRows(this.rows)) {
        body.append(this.sessionButton(row));
      }
    }
    shell.append(header, body);
    this.root.hidden = false;
    this.root.replaceChildren(shell);
  }

  private sessionButton(row: SessionSummary): HTMLButtonElement {
    const el = document.createElement("button");
    el.type = "button";
    el.className = sessionRowClass(row);
    el.title = row.id;
    const top = document.createElement("span");
    top.className = "session-list-top";
    const agent = document.createElement("span");
    agent.className = "session-list-agent";
    agent.textContent = sessionTitle(row);
    const id = document.createElement("span");
    id.className = "session-list-id";
    id.textContent = shortID(row.id);
    top.append(agent, id);

    const cwd = document.createElement("span");
    cwd.className = "session-list-cwd";
    cwd.textContent = row.remote === true && row.remote_url !== undefined ? row.remote_url : row.cwd;

    const meta = document.createElement("span");
    meta.className = "session-list-meta";
    meta.textContent = sessionMeta(row);

    el.append(top, cwd, meta);
    el.addEventListener("click", () => {
      if (row.remote_url !== undefined && row.remote_url !== "") {
        window.location.href = row.remote_url;
        return;
      }
      saveLastSessionID(row.id);
      this.navigate(row.id);
    });
    return el;
  }
}

export class SessionsPanel {
  private readonly sessions = new Map<string, SessionCostPayload | undefined>();

  constructor(
    private readonly root: HTMLElement,
    private readonly currentSessionID: string,
    private readonly fetchJSON: FetchJSON
  ) {
    this.addSession(currentSessionID);
    void this.loadCost(currentSessionID);
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "session.started") {
      const payload = envelope.payload as { session_id?: string };
      if (payload.session_id !== undefined && payload.session_id !== "") {
        this.addSession(payload.session_id);
        void this.loadCost(payload.session_id);
      }
      return;
    }
    if (envelope.type === "cost.updated") {
      const payload = envelope.payload as { session_id?: string };
      if (payload.session_id !== undefined && payload.session_id !== "") {
        this.addSession(payload.session_id);
        void this.loadCost(payload.session_id);
      }
    }
  }

  private addSession(id: string): void {
    if (!this.sessions.has(id)) {
      this.sessions.set(id, undefined);
      this.render();
    }
  }

  private async loadCost(id: string): Promise<void> {
    try {
      const cost = await this.fetchJSON<SessionCostPayload>(`/sessions/${encodeURIComponent(id)}/cost`);
      this.sessions.set(id, cost);
      this.render();
    } catch {
      this.render();
    }
  }

  private render(): void {
    const current = this.sessions.get(this.currentSessionID);
    const header = document.createElement("div");
    header.className = "sessions-current";
    header.textContent = `daily ${formatTokens(current?.daily_tokens ?? 0)}`;

    const list = document.createElement("div");
    list.className = "sessions-list";
    for (const [id, cost] of this.sessions) {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = id === this.currentSessionID ? "session-tab active" : "session-tab";
      tab.title = id;
      const name = document.createElement("span");
      name.className = "session-name";
      name.textContent = shortID(id);
      const value = document.createElement("span");
      value.className = "session-cost";
      value.textContent = formatCost(cost);
      tab.append(name, value);
      tab.addEventListener("click", () => {
        if (id !== this.currentSessionID) {
          saveLastSessionID(id);
          window.location.href = `/s/${encodeURIComponent(id)}`;
        }
      });
      list.append(tab);
    }
    this.root.hidden = false;
    this.root.replaceChildren(header, list);
  }
}

export function saveLastSessionID(id: string): void {
  if (id.trim() !== "") {
    window.localStorage.setItem(lastSessionKey, id);
  }
}

function lastSessionID(): string {
  return window.localStorage.getItem(lastSessionKey) ?? "";
}

function sortedRows(rows: SessionSummary[]): SessionSummary[] {
  const last = lastSessionID();
  return [...rows].sort((a, b) => {
    if (a.id === last) {
      return -1;
    }
    if (b.id === last) {
      return 1;
    }
    return sessionTime(b) - sessionTime(a);
  });
}

function sessionMeta(row: SessionSummary): string {
  if (row.remote === true) {
    return "remote";
  }
  const parts = [`${formatTokens(row.tokens_used)}`];
  if (row.cost_usd > 0) {
    parts.push(formatUSD(row.cost_usd));
  }
  if (row.pending_approvals_count > 0) {
    parts.push(`${row.pending_approvals_count} pending`);
  }
  parts.push(formatWhen(row.last_activity));
  parts.push(row.role_required);
  return parts.join(" / ");
}

function sessionRowClass(row: SessionSummary): string {
  const classes = ["session-list-row"];
  if (row.id === lastSessionID()) {
    classes.push("last");
  }
  if (row.remote === true) {
    classes.push("remote");
  }
  return classes.join(" ");
}

function sessionTitle(row: SessionSummary): string {
  if (row.remote === true) {
    return (row.peer_name ?? row.agent) || "remote";
  }
  return row.agent || "session";
}

function sessionTime(row: SessionSummary): number {
  const ts = Date.parse(row.last_activity);
  return Number.isFinite(ts) ? ts : 0;
}

function emptyRow(text: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "session-list-empty";
  el.textContent = text;
  return el;
}

function formatCost(cost: SessionCostPayload | undefined): string {
  if (cost === undefined) {
    return "0 tok";
  }
  if (cost.cost_known && cost.total_usd !== undefined) {
    return formatUSD(cost.total_usd);
  }
  return formatTokens(cost.total_tokens);
}

function formatUSD(value: number): string {
  const digits = value > 0 && value < 0.01 ? 4 : 2;
  return `$${value.toFixed(digits)}`;
}

function formatTokens(value: number): string {
  return `${Math.max(0, Math.round(value)).toLocaleString("en-US")} tok`;
}

function formatWhen(raw: string): string {
  const ts = Date.parse(raw);
  if (!Number.isFinite(ts)) {
    return "-";
  }
  return new Date(ts).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function shortID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}
