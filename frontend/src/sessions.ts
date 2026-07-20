import type { EventEnvelope } from "./events";

const lastSessionKey = "onibi-last-session-id";

export type SessionSummary = {
  id: string;
  agent: string;
  cwd: string;
  started_at: string;
  last_activity: string;
  pending_approvals_count: number;
  recovery_state?: SessionRecoveryState;
  recovery_reason?: string;
  recovery_updated_at?: string;
  role_required: string;
};

type SessionRecoveryState =
  "healthy" | "reconnecting" | "recovering" | "orphaned" | "failed" | "terminated";

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

export class SessionsListView {
  private rows: SessionSummary[] = [];
  private loading = false;
  private status = "";
  private readonly killArmed = new Map<string, number>();
  private suppressAttachUntil = 0;

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly navigate: (sessionID: string) => void,
    private readonly postJSON?: PostJSON,
    private readonly toast?: (message: string) => void,
    private readonly headerControl?: HTMLElement
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

  handleEnvelope(envelope: EventEnvelope): void {
    if (
      envelope.type === "session.started" ||
      envelope.type === "session.ended" ||
      envelope.type === "session.activity" ||
      envelope.type === "approval.requested" ||
      envelope.type === "approval.decided" ||
      envelope.type === "approval.expired"
    ) {
      void this.load();
      return;
    }
  }

  private sessionsPath(): string {
    return "/sessions";
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
    if (this.headerControl !== undefined) {
      header.append(this.headerControl);
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
        body.append(this.sessionRow(row));
      }
    }
    const status = document.createElement("div");
    status.className = "session-list-status";
    status.textContent = this.status;
    shell.append(header, body, status);
    this.root.hidden = false;
    this.root.replaceChildren(shell);
  }

  private sessionRow(row: SessionSummary): HTMLElement {
    const el = document.createElement("div");
    el.role = "button";
    el.tabIndex = 0;
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
    cwd.textContent = row.cwd;

    const meta = document.createElement("span");
    meta.className = "session-list-meta";
    meta.textContent = sessionMeta(row);

    const actions = document.createElement("span");
    actions.className = "session-list-actions";
    const attachable = canAttach(row);
    const attach = sessionAction(attachable ? "Attach" : recoveryAction(row));
    attach.disabled = !attachable;
    if (attachable) {
      attach.addEventListener("click", (event) => {
        event.stopPropagation();
        this.attach(row);
      });
    }
    actions.append(attach);
    if (this.postJSON !== undefined) {
      const kill = sessionAction(
        this.killArmedUntil(row.id) > Date.now() ? "Confirm KILL" : "KILL"
      );
      kill.classList.add("danger");
      kill.addEventListener("click", (event) => {
        event.stopPropagation();
        void this.kill(row);
      });
      actions.append(kill);
    }

    el.append(top, cwd, meta, actions);
    el.addEventListener("click", (event) => {
      if (Date.now() < this.suppressAttachUntil) {
        return;
      }
      if ((event.target as Element | null)?.closest("button") !== null) {
        return;
      }
      if (attachable) {
        this.attach(row);
      }
    });
    el.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        if (attachable) {
          this.attach(row);
        }
      }
    });
    this.installLongPressKill(el, row);
    return el;
  }

  private attach(row: SessionSummary): void {
    saveLastSessionID(row.id);
    this.navigate(row.id);
  }

  private installLongPressKill(el: HTMLElement, row: SessionSummary): void {
    if (this.postJSON === undefined) {
      return;
    }
    let timer = 0;
    const clear = () => {
      window.clearTimeout(timer);
      timer = 0;
    };
    el.addEventListener("pointerdown", (event) => {
      if ((event.target as Element | null)?.closest("button") !== null) {
        return;
      }
      clear();
      timer = window.setTimeout(() => {
        this.suppressAttachUntil = Date.now() + 700;
        this.armKill(row.id);
        this.render();
      }, 650);
    });
    el.addEventListener("pointerup", clear);
    el.addEventListener("pointerleave", clear);
    el.addEventListener("pointercancel", clear);
  }

  private async kill(row: SessionSummary): Promise<void> {
    if (this.postJSON === undefined) {
      return;
    }
    if (this.killArmedUntil(row.id) <= Date.now()) {
      this.armKill(row.id);
      this.render();
      return;
    }
    this.killArmed.delete(row.id);
    this.status = `killing ${shortID(row.id)}`;
    this.render();
    try {
      const response = await this.postJSON("/control", { session_id: row.id, action: "kill" });
      if (!response.ok) {
        throw new Error((await response.text()).trim() || `kill ${response.status}`);
      }
      this.toast?.(`Killed ${shortID(row.id)}.`);
      await this.load();
    } catch (err) {
      this.status = err instanceof Error ? err.message : "kill failed";
      this.render();
    }
  }

  private armKill(id: string): void {
    this.killArmed.set(id, Date.now() + 2500);
    this.status = `tap KILL again to kill ${shortID(id)}`;
  }

  private killArmedUntil(id: string): number {
    return this.killArmed.get(id) ?? 0;
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
  const recovery = recoveryText(row);
  const parts = recovery === undefined ? [] : [recovery];
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
  if (hasUnhealthyRecovery(row)) {
    classes.push("recovery-unhealthy");
  }
  return classes.join(" ");
}

function sessionTitle(row: SessionSummary): string {
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

function sessionAction(label: string): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "session-list-action";
  el.textContent = label;
  el.tabIndex = -1;
  return el;
}

function formatWhen(raw: string): string {
  const ts = Date.parse(raw);
  if (!Number.isFinite(ts)) {
    return "-";
  }
  return new Date(ts).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function shortID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}

function hasUnhealthyRecovery(row: SessionSummary): boolean {
  return row.recovery_state !== undefined && row.recovery_state !== "healthy";
}

function recoveryText(row: SessionSummary): string | undefined {
  if (!hasUnhealthyRecovery(row)) {
    return undefined;
  }
  const reason = row.recovery_reason?.trim();
  return reason === undefined || reason === ""
    ? `recovery ${row.recovery_state}`
    : `recovery ${row.recovery_state}: ${reason}`;
}

function canAttach(row: SessionSummary): boolean {
  return !hasUnhealthyRecovery(row);
}

function recoveryAction(row: SessionSummary): string {
  return row.recovery_state === undefined ? "Unavailable" : row.recovery_state;
}
