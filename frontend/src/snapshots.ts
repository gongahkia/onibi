import type { EventEnvelope } from "./events";

export type SnapshotItem = {
  name: string;
  session_id: string;
  created_at: string;
  cwd: string;
  transcript_offset: number;
};

type SnapshotListResponse = {
  snapshots: SnapshotItem[];
};

type SnapshotActionResult = {
  session_id: string;
  message: string;
};

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

export class SnapshotsPanel {
  private items: SnapshotItem[] = [];
  private open = false;
  private loading = false;
  private status = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly currentSessionID: string,
    private readonly fetchJSON: FetchJSON,
    private readonly postJSON: PostJSON,
    private readonly navigateToSession: (sessionID: string) => void,
    private readonly toast: (message: string) => void
  ) {}

  toggle(): void {
    this.open = !this.open;
    this.render();
    if (this.open) {
      void this.refresh();
    }
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "snapshot.created" || envelope.type === "session.started") {
      void this.refresh();
    }
  }

  private async refresh(): Promise<void> {
    this.loading = true;
    this.status = "";
    this.render();
    try {
      const response = await this.fetchJSON<SnapshotListResponse>("/snapshots");
      this.items = response.snapshots;
    } catch {
      this.status = "snapshots unavailable";
    } finally {
      this.loading = false;
      this.render();
    }
  }

  private render(): void {
    if (!this.open) {
      this.root.hidden = true;
      this.root.replaceChildren();
      return;
    }
    const header = document.createElement("div");
    header.className = "snapshots-header";
    const title = document.createElement("div");
    title.className = "snapshots-title";
    title.textContent = "snapshots";
    const reload = button("Reload", "secondary");
    reload.addEventListener("click", () => void this.refresh());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => {
      this.open = false;
      this.render();
    });
    header.append(title, reload, close);

    const body = document.createElement("div");
    body.className = "snapshots-list";
    if (this.loading) {
      const row = document.createElement("div");
      row.className = "snapshot-empty";
      row.textContent = "loading";
      body.append(row);
    } else if (this.items.length === 0) {
      const row = document.createElement("div");
      row.className = "snapshot-empty";
      row.textContent = this.status || "no snapshots";
      body.append(row);
    } else {
      for (const item of this.items) {
        body.append(this.snapshotRow(item));
      }
    }
    const status = document.createElement("div");
    status.className = "snapshots-status";
    status.textContent = this.status;
    this.root.hidden = false;
    this.root.replaceChildren(header, body, status);
  }

  private snapshotRow(item: SnapshotItem): HTMLButtonElement {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "snapshot-row";
    row.title = item.name;
    const name = document.createElement("span");
    name.className = "snapshot-name";
    name.textContent = item.name;
    const meta = document.createElement("span");
    meta.className = "snapshot-meta";
    meta.textContent = `${formatWhen(item.created_at)} / ${shortID(item.session_id)}`;
    const cwd = document.createElement("span");
    cwd.className = "snapshot-cwd";
    cwd.textContent = item.cwd;
    row.append(name, meta, cwd);

    let timer = 0;
    let forked = false;
    const clear = () => {
      window.clearTimeout(timer);
      timer = 0;
    };
    row.addEventListener("pointerdown", () => {
      forked = false;
      clear();
      timer = window.setTimeout(() => {
        forked = true;
        this.openFork(item);
      }, 550);
    });
    row.addEventListener("pointerup", () => {
      clear();
      if (!forked) {
        void this.restore(item);
      }
    });
    row.addEventListener("pointerleave", clear);
    row.addEventListener("pointercancel", clear);
    row.addEventListener("contextmenu", (event) => {
      event.preventDefault();
      clear();
      this.openFork(item);
    });
    return row;
  }

  private async restore(item: SnapshotItem): Promise<void> {
    this.status = `restoring ${item.name}`;
    this.render();
    try {
      const result = await this.postResult("/snapshots/restore", {
        name: item.name,
        session_id: this.currentSessionID
      });
      this.toast(result.message || "restored");
      this.navigateToSession(result.session_id);
    } catch (err) {
      this.status = err instanceof Error ? err.message : "restore failed";
      this.render();
    }
  }

  private openFork(item: SnapshotItem): void {
    const modal = document.createElement("div");
    modal.className = "snapshot-modal";
    const form = document.createElement("form");
    form.className = "snapshot-form";
    const title = document.createElement("div");
    title.className = "snapshot-form-title";
    title.textContent = item.name;
    const turn = document.createElement("input");
    turn.type = "number";
    turn.min = "0";
    turn.step = "1";
    turn.value = "0";
    turn.inputMode = "numeric";
    turn.className = "snapshot-turn";
    turn.ariaLabel = "turn";
    const prompt = document.createElement("textarea");
    prompt.className = "snapshot-prompt";
    prompt.placeholder = "new prompt";
    prompt.required = true;
    const actions = document.createElement("div");
    actions.className = "snapshot-form-actions";
    const cancel = button("Cancel", "secondary");
    cancel.type = "button";
    const submit = button("Fork", "primary");
    submit.type = "submit";
    const formStatus = document.createElement("div");
    formStatus.className = "snapshot-form-status";
    actions.append(cancel, submit);
    form.append(title, turn, prompt, actions, formStatus);
    modal.append(form);
    cancel.addEventListener("click", () => modal.remove());
    modal.addEventListener("click", (event) => {
      if (event.target === modal) {
        modal.remove();
      }
    });
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      void this.fork(item, Number(turn.value), prompt.value, modal, formStatus);
    });
    this.root.append(modal);
    prompt.focus();
  }

  private async fork(
    item: SnapshotItem,
    turn: number,
    prompt: string,
    modal: HTMLElement,
    status: HTMLElement
  ): Promise<void> {
    if (!Number.isFinite(turn) || turn < 0 || prompt.trim() === "") {
      return;
    }
    status.textContent = `forking ${item.name}`;
    try {
      const result = await this.postResult("/snapshots/fork", {
        name: item.name,
        session_id: this.currentSessionID,
        turn: Math.trunc(turn),
        new_prompt: prompt
      });
      modal.remove();
      this.toast(result.message || "forked");
      this.navigateToSession(result.session_id);
    } catch (err) {
      status.textContent = err instanceof Error ? err.message : "fork failed";
    }
  }

  private async postResult(
    path: string,
    body: Record<string, unknown>
  ): Promise<SnapshotActionResult> {
    const response = await this.postJSON(path, body);
    const text = await response.text();
    if (!response.ok) {
      throw new Error(text.trim() || `${path} ${response.status}`);
    }
    return JSON.parse(text) as SnapshotActionResult;
  }
}

function button(label: string, variant: "primary" | "secondary"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${variant}`;
  el.textContent = label;
  return el;
}

function formatWhen(raw: string): string {
  const ts = Date.parse(raw);
  if (!Number.isFinite(ts)) {
    return raw;
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
