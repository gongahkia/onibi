import type { AnomalyRequestedPayload, ApprovalDecidedPayload, EventEnvelope } from "./events";

type PostJSON = (path: string, body: Record<string, string>) => Promise<Response>;

type AnomalyCard = {
  payload: AnomalyRequestedPayload;
  element: HTMLElement;
  allowButton: HTMLButtonElement;
};

export class AnomalyOverlay {
  private cards = new Map<string, AnomalyCard>();
  private postJSON: PostJSON = defaultPostJSON;

  constructor(private readonly root: HTMLElement) {}

  setPostJSON(postJSON: PostJSON): void {
    this.postJSON = postJSON;
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "anomaly.requested") {
      this.add(envelope.payload as AnomalyRequestedPayload);
      return;
    }
    if (envelope.type === "approval.decided" || envelope.type === "approval.expired") {
      this.remove((envelope.payload as ApprovalDecidedPayload).id);
    }
  }

  private add(payload: AnomalyRequestedPayload): void {
    this.remove(payload.approval_id);
    dismissKeyboard();
    const card = document.createElement("section");
    card.className = "approval-card anomaly-card risk-high";
    card.dataset.id = payload.approval_id;

    const header = document.createElement("div");
    header.className = "approval-header";
    const title = document.createElement("div");
    title.className = "approval-title";
    title.textContent = payload.rule_name;
    const meta = document.createElement("div");
    meta.className = "approval-meta";
    meta.textContent = `${payload.agent ?? "agent"} / ${payload.session_id}`;
    const badge = document.createElement("div");
    badge.className = "approval-risk";
    badge.textContent = payload.paused ? "paused" : "warn";
    header.append(title, meta, badge);

    const evidence = document.createElement("pre");
    evidence.className = "approval-input anomaly-evidence";
    evidence.textContent = payload.evidence;

    const actions = document.createElement("div");
    actions.className = "approval-actions";
    const resume = button("Resume", "primary");
    const kill = button("Kill", "danger");
    const allow = button("Always-allow", "secondary");
    actions.append(resume, kill, allow);

    const status = document.createElement("div");
    status.className = "approval-status";
    card.append(header, evidence, actions, status);
    this.root.append(card);
    window.setTimeout(() => card.scrollIntoView({ block: "nearest", inline: "nearest" }), 50);
    this.cards.set(payload.approval_id, { payload, element: card, allowButton: allow });

    resume.addEventListener("click", () => {
      vibrate();
      void this.decide(payload.approval_id, { verdict: "approve" }, status);
    });
    kill.addEventListener("click", () => {
      vibrate();
      void this.kill(payload, status);
    });
    allow.addEventListener("click", () => {
      vibrate();
      void this.allow(payload, allow, status);
    });
  }

  private remove(id: string): void {
    const card = this.cards.get(id);
    if (card === undefined) {
      return;
    }
    card.element.remove();
    this.cards.delete(id);
  }

  private async decide(id: string, body: Record<string, string>, status: HTMLElement): Promise<void> {
    status.textContent = "Sending...";
    const response = await this.postJSON(`/approval/${encodeURIComponent(id)}`, body);
    if (!response.ok) {
      status.textContent = await response.text();
      return;
    }
    status.textContent = "Done.";
  }

  private async kill(payload: AnomalyRequestedPayload, status: HTMLElement): Promise<void> {
    status.textContent = "Killing...";
    const killed = await this.postJSON("/control", { session_id: payload.session_id, action: "kill" });
    if (!killed.ok) {
      status.textContent = await killed.text();
      return;
    }
    await this.decide(payload.approval_id, { verdict: "deny", reason: "killed from anomaly card" }, status);
  }

  private async allow(payload: AnomalyRequestedPayload, button: HTMLButtonElement, status: HTMLElement): Promise<void> {
    status.textContent = "Adding allowlist...";
    button.disabled = true;
    const response = await this.postJSON("/anomaly/allowlist", {
      session_id: payload.session_id,
      rule_name: payload.rule_name,
      evidence: payload.evidence
    });
    if (!response.ok) {
      status.textContent = await response.text();
      button.disabled = false;
      return;
    }
    status.textContent = "Allowlist updated.";
  }
}

async function defaultPostJSON(path: string, body: Record<string, string>): Promise<Response> {
  return fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
}

function button(label: string, kind: string): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${kind}`;
  el.textContent = label;
  return el;
}

function vibrate(): void {
  navigator.vibrate?.(12);
}

function dismissKeyboard(): void {
  if (document.activeElement instanceof HTMLElement) {
    document.activeElement.blur();
  }
  document.querySelectorAll<HTMLElement>(".xterm-helper-textarea").forEach((el) => el.blur());
}
