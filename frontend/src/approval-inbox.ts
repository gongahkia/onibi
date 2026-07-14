import type { EventEnvelope } from "./events";
import type { FleetApproval, FleetHost, FleetSession, FleetStatus } from "./fleet-hosts";

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

type ApprovalProvenance = {
  host: string;
  actionable: boolean;
};

export class ApprovalInboxPanel {
  readonly element = document.createElement("button");
  private modal: HTMLElement | undefined;
  private payload: FleetStatus | undefined;
  private loading = false;
  private status = "";
  private approveArmedID = "";

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
    } catch {
      this.payload = undefined;
      this.status = "approval inbox unavailable";
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
    if (this.loading) {
      return empty("loading approval inbox");
    }
    if (this.payload === undefined) {
      return empty(this.status || "approval inbox unavailable");
    }
    if (this.payload.pending_approvals.length === 0) {
      return empty("no pending approvals");
    }
    const list = document.createElement("div");
    list.className = "approval-inbox-list";
    for (const approval of this.payload.pending_approvals) {
      list.append(this.approvalCard(approval));
    }
    return list;
  }

  private approvalCard(approval: FleetApproval): HTMLElement {
    const provenance = this.provenance(approval);
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
      sourceValue("host", provenance.host),
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
    if (!provenance.actionable) {
      approve.disabled = true;
      deny.disabled = true;
      approve.title = "host provenance not reported";
      deny.title = "host provenance not reported";
    } else {
      approve.addEventListener("click", () => void this.approve(approval.id));
      deny.addEventListener("click", () => void this.decide(approval.id, "deny"));
    }
    actions.append(approve, deny);
    card.append(heading, source, actions);
    return card;
  }

  private provenance(approval: FleetApproval): ApprovalProvenance {
    const host = this.host(approval.host_id);
    if (host !== undefined) {
      return { host: `${host.display_name} / ${host.id}`, actionable: true };
    }
    if (approval.host_id !== undefined && approval.host_id !== "") {
      return { host: "not reported", actionable: false };
    }
    const session = this.session(approval.session_id);
    if (session !== undefined && session.remote !== true && session.host_id === undefined) {
      return { host: "this hub", actionable: true };
    }
    return { host: "not reported", actionable: false };
  }

  private host(id: string | undefined): FleetHost | undefined {
    return this.payload?.hosts.find((host) => host.id === id);
  }

  private session(id: string): FleetSession | undefined {
    return this.payload?.sessions.find((session) => session.id === id);
  }

  private async approve(id: string): Promise<void> {
    if (this.approveArmedID !== id) {
      this.approveArmedID = id;
      this.status = `tap Approve again for ${id}`;
      this.render();
      return;
    }
    await this.decide(id, "approve");
  }

  private async decide(id: string, verdict: "approve" | "deny"): Promise<void> {
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
