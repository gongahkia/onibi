import type { SessionsStatusPayload, SessionStatus } from "./session-status";

type FetchJSON = <T>(path: string) => Promise<T>;

export class SessionPickerPanel {
  private modal: HTMLElement | undefined;
  private payload: SessionsStatusPayload | undefined;
  private loading = false;
  private status = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly navigate: (session: SessionStatus) => void
  ) {}

  open(): void {
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
      this.payload = await this.fetchJSON<SessionsStatusPayload>("/sessions/status?include=remote");
    } catch {
      this.payload = undefined;
      this.status = "session picker unavailable";
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
    shell.className = "share-form session-picker-form";
    const header = document.createElement("div");
    header.className = "share-header";
    const title = document.createElement("div");
    title.className = "share-title";
    title.textContent = "session picker";
    const reload = button("Reload");
    reload.addEventListener("click", () => void this.load());
    const close = button("Close");
    close.addEventListener("click", () => this.close());
    header.append(title, reload, close);
    shell.append(header, this.body());
    this.modal.replaceChildren(shell);
  }

  private body(): HTMLElement {
    if (this.loading) {
      return empty("loading sessions");
    }
    if (this.payload === undefined) {
      return empty(this.status || "session picker unavailable");
    }
    if (this.payload.sessions.length === 0) {
      return empty("no sessions");
    }
    const list = document.createElement("div");
    list.className = "session-picker-list";
    for (const session of sorted(this.payload.sessions)) {
      const row = document.createElement("button");
      row.type = "button";
      row.className = `session-picker-row state-${session.state}`;
      row.title = session.id;
      const title = document.createElement("span");
      title.textContent = `${session.agent} / ${session.state}`;
      const meta = document.createElement("span");
      meta.textContent = `${session.last_activity} / ${session.id}`;
      row.append(title, meta);
      row.addEventListener("click", () => this.navigate(session));
      list.append(row);
    }
    return list;
  }
}

function sorted(sessions: SessionStatus[]): SessionStatus[] {
  return [...sessions].sort(
    (left, right) => stateRank(left.state) - stateRank(right.state) || recent(right) - recent(left)
  );
}

function stateRank(state: SessionStatus["state"]): number {
  return ["failed", "recovering", "awaiting-approval", "blocked", "working", "idle"].indexOf(state);
}

function recent(session: SessionStatus): number {
  const value = Date.parse(session.last_activity);
  return Number.isFinite(value) ? value : 0;
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
