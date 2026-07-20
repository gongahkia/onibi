import type { EventEnvelope } from "./events";

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

export type PendingApproval = {
  id: string;
  session_id: string;
  agent: string;
  tool: string;
  expires_at: string;
};

export type PendingApprovals = { approvals: PendingApproval[] };

export class ApprovalInboxPanel {
  readonly element = document.createElement("button");
  private modal: HTMLElement | undefined;
  private payload: PendingApprovals | undefined;
  private loading = false;
  private status = "";
  private approveArmedID = "";
  private decidingID = "";
  private offline = false;

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly postJSON: PostJSON,
    private readonly toast: (message: string) => void
  ) {
    this.element.type = "button";
    this.element.className = "control-button";
    this.element.textContent = "Inbox";
    this.element.addEventListener("click", () => this.open());
  }

  open(): void {
    this.status = "";
    this.approveArmedID = "";
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

  handleEnvelope(envelope: EventEnvelope): void {
    if (
      this.modal !== undefined &&
      (envelope.type === "approval.requested" ||
        envelope.type === "approval.decided" ||
        envelope.type === "approval.expired")
    ) {
      void this.load();
    }
  }

  setOffline(offline: boolean): void {
    this.offline = offline;
    if (this.modal === undefined) {
      return;
    }
    if (offline) {
      this.status =
        this.payload === undefined
          ? "offline: reconnect then reload"
          : "offline: showing cached approvals";
      this.render();
      return;
    }
    void this.load();
  }

  private close(): void {
    this.modal?.remove();
    this.modal = undefined;
  }

  private async load(): Promise<void> {
    if (this.offline) {
      this.status =
        this.payload === undefined
          ? "offline: reconnect then reload"
          : "offline: showing cached approvals";
      this.render();
      return;
    }
    this.loading = true;
    this.status = this.payload === undefined ? "" : "refreshing approvals";
    this.render();
    try {
      this.payload = await this.fetchJSON<PendingApprovals>("/approvals/pending");
      this.status = "";
    } catch {
      this.status =
        this.payload === undefined
          ? "approval inbox unavailable"
          : "approval data may be stale: reconnect then reload";
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
    shell.className = "share-form approval-inbox-form";
    const header = document.createElement("div");
    header.className = "share-header";
    const title = document.createElement("div");
    title.className = "share-title";
    title.textContent = "approval inbox";
    const reload = button("Reload", "secondary");
    reload.addEventListener("click", () => void this.load());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => this.close());
    header.append(title, reload, close);
    shell.append(header, this.body(), statusLine(this.status));
    this.modal.replaceChildren(shell);
  }

  private body(): HTMLElement {
    if (this.loading && this.payload === undefined) {
      return empty("loading approval inbox");
    }
    if (this.payload === undefined) {
      return empty(this.status || "approval inbox unavailable");
    }
    if (this.payload.approvals.length === 0) {
      return empty("no pending approvals");
    }
    const list = document.createElement("div");
    list.className = "approval-inbox-list";
    for (const approval of this.payload.approvals) {
      list.append(this.approvalCard(approval));
    }
    return list;
  }

  private approvalCard(approval: PendingApproval): HTMLElement {
    const card = document.createElement("section");
    card.className = "approval-inbox-card";
    card.dataset.approvalId = approval.id;
    const heading = document.createElement("div");
    heading.className = "approval-inbox-heading";
    const tool = document.createElement("strong");
    tool.textContent = approval.tool;
    const expires = document.createElement("span");
    expires.textContent = `expires ${approval.expires_at}`;
    heading.append(tool, expires);
    const source = document.createElement("div");
    source.className = "approval-inbox-source";
    source.append(
      sourceValue("session", approval.session_id),
      sourceValue("agent", approval.agent),
      sourceValue("approval", approval.id)
    );
    const actions = document.createElement("div");
    actions.className = "approval-inbox-actions";
    const approve = button(
      this.approveArmedID === approval.id ? "Confirm approve" : "Approve",
      "primary"
    );
    const deny = button("Deny", "danger");
    if (this.decidingID !== "") {
      approve.disabled = true;
      deny.disabled = true;
      approve.title = "approval decision pending";
      deny.title = "approval decision pending";
    } else {
      approve.addEventListener("click", () => void this.approve(approval.id));
      deny.addEventListener("click", () => void this.decide(approval.id, "deny"));
    }
    actions.append(approve, deny);
    card.append(heading, source, actions);
    return card;
  }

  private async approve(id: string): Promise<void> {
    if (this.decidingID !== "") {
      return;
    }
    if (this.approveArmedID !== id) {
      this.approveArmedID = id;
      this.status = `tap Approve again for ${id}`;
      this.render();
      return;
    }
    await this.decide(id, "approve");
  }

  private async decide(id: string, verdict: "approve" | "deny"): Promise<void> {
    if (this.decidingID !== "") {
      return;
    }
    this.decidingID = id;
    this.approveArmedID = "";
    this.status = `sending ${verdict}`;
    this.render();
    try {
      const response = await this.postJSON(`/approval/${encodeURIComponent(id)}`, { verdict });
      if (!response.ok) {
        throw new Error((await response.text()).trim() || `approval ${response.status}`);
      }
      this.toast(`Approval ${verdict} sent.`);
      await this.load();
    } catch (error) {
      this.status = error instanceof Error ? error.message : "approval action failed";
      this.render();
    } finally {
      this.decidingID = "";
      this.render();
    }
  }
}

function sourceValue(label: string, value: string): HTMLElement {
  const row = document.createElement("span");
  row.className = "approval-inbox-source-value";
  row.textContent = `${label}: ${value}`;
  return row;
}

function empty(message: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-empty";
  el.textContent = message;
  return el;
}

function statusLine(message: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-status";
  el.textContent = message;
  return el;
}

function button(label: string, kind: "primary" | "secondary" | "danger"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${kind}`;
  el.textContent = label;
  return el;
}
