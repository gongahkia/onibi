import type { ApprovalDecidedPayload, ApprovalRequestedPayload, EventEnvelope } from "./events";
import type { ApprovalWakeLock } from "./wake-lock";

type Diff2HtmlUIModule = typeof import("diff2html/lib/ui/js/diff2html-ui-slim.js");

const maxInlineDiffLines = 200;
const maxInlineDiffBytes = 50 * 1024;

type ApprovalCard = {
  payload: ApprovalRequestedPayload;
  element: HTMLElement;
  approveUntil: number;
  trustTimer?: number;
  trustButton?: HTMLButtonElement;
};

type PostJSON = (path: string, body: Record<string, string>) => Promise<Response>;

type TrustScope = {
  label: string;
  pattern: string;
};

export class ApprovalOverlay {
  private cards = new Map<string, ApprovalCard>();
  private postJSON: PostJSON = defaultPostJSON;

  constructor(
    private readonly root: HTMLElement,
    private readonly wakeLock?: ApprovalWakeLock
  ) {}

  setPostJSON(postJSON: PostJSON): void {
    this.postJSON = postJSON;
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "approval.requested") {
      this.add(envelope.payload as ApprovalRequestedPayload);
      return;
    }
    if (envelope.type === "approval.decided" || envelope.type === "approval.expired") {
      this.remove((envelope.payload as ApprovalDecidedPayload).id);
    }
  }

  private add(payload: ApprovalRequestedPayload): void {
    this.remove(payload.id);
    dismissKeyboard();
    const card = document.createElement("section");
    card.className = `approval-card risk-${payload.risk_level}`;
    card.dataset.id = payload.id;

    const header = document.createElement("div");
    header.className = "approval-header";
    const title = document.createElement("div");
    title.className = "approval-title";
    title.textContent = payload.tool;
    const meta = document.createElement("div");
    meta.className = "approval-meta";
    meta.textContent = `${payload.agent} / ${payload.session_id}`;
    const badge = document.createElement("div");
    badge.className = "approval-risk";
    badge.textContent = payload.risk_level;
    header.append(title, meta, badge);

    const input = document.createElement("div");
    input.className = "approval-input";
    if (payload.unified_diff !== undefined && payload.unified_diff !== "") {
      input.classList.add("approval-diff");
      prepareUnifiedDiff(input, payload.unified_diff);
    } else {
      input.append(...lineNodes(payload.scrubbed_input));
    }
    const budget = budgetWarningNode(payload);

    const actions = document.createElement("div");
    actions.className = "approval-actions";
    const approve = button(
      payload.risk_level === "high" ? "Approve (tap twice)" : "Approve",
      "primary"
    );
    const deny = button("Deny", "danger");
    const edit = button("Edit", "secondary");
    const scope = runtimeTrustScope(payload.file_path);
    const trust =
      scope === undefined
        ? undefined
        : button(`Auto-approve all ${payload.tool} in ${scope.label} for 5min`, "secondary");
    trust?.classList.add("approval-trust-link");
    actions.append(approve, deny, edit);
    if (trust !== undefined) {
      actions.append(trust);
    }

    const editPane = document.createElement("form");
    editPane.className = "approval-edit";
    editPane.hidden = true;
    const textarea = document.createElement("textarea");
    textarea.value = payload.scrubbed_input;
    const submit = button("Submit edit", "primary");
    submit.type = "submit";
    const cancel = button("Cancel", "secondary");
    cancel.type = "button";
    editPane.append(textarea, submit, cancel);

    const status = document.createElement("div");
    status.className = "approval-status";
    const trustChip = document.createElement("span");
    trustChip.className = "approval-trust-chip";
    trustChip.hidden = true;
    if (budget !== undefined) {
      card.append(header, budget, input, actions, editPane, trustChip, status);
    } else {
      card.append(header, input, actions, editPane, trustChip, status);
    }
    this.root.append(card);
    window.setTimeout(() => card.scrollIntoView({ block: "nearest", inline: "nearest" }), 50);
    const tracked: ApprovalCard = { payload, element: card, approveUntil: 0, trustButton: trust };
    this.cards.set(payload.id, tracked);
    this.updateWakeLock();

    approve.addEventListener("click", () => {
      vibrate();
      if (payload.risk_level === "high") {
        const now = Date.now();
        if (tracked.approveUntil < now) {
          tracked.approveUntil = now + 2000;
          status.textContent = "Tap again to approve.";
          return;
        }
      }
      void this.decide(payload.id, { verdict: "approve" }, status);
    });
    deny.addEventListener("click", () => {
      vibrate();
      void this.decide(payload.id, { verdict: "deny" }, status);
    });
    edit.addEventListener("click", () => {
      vibrate();
      editPane.hidden = false;
      textarea.focus();
    });
    trust?.addEventListener("click", () => {
      if (scope === undefined) {
        return;
      }
      vibrate();
      void this.addRuntimeTrust(payload, scope, tracked, trustChip, status);
    });
    cancel.addEventListener("click", () => {
      editPane.hidden = true;
      status.textContent = "";
    });
    editPane.addEventListener("submit", (event) => {
      event.preventDefault();
      vibrate();
      void this.decide(payload.id, { verdict: "edit", edited_input: textarea.value }, status);
    });
  }

  private remove(id: string): void {
    const card = this.cards.get(id);
    if (card === undefined) {
      return;
    }
    if (card.trustTimer !== undefined) {
      window.clearInterval(card.trustTimer);
    }
    card.element.remove();
    this.cards.delete(id);
    this.updateWakeLock();
  }

  private updateWakeLock(): void {
    this.wakeLock?.setPendingCount(this.cards.size);
  }

  private async decide(
    id: string,
    body: Record<string, string>,
    status: HTMLElement
  ): Promise<void> {
    status.textContent = "Sending...";
    const response = await this.postJSON(`/approval/${encodeURIComponent(id)}`, body);
    if (!response.ok) {
      status.textContent = await response.text();
      return;
    }
    status.textContent = "Done.";
  }

  private async addRuntimeTrust(
    payload: ApprovalRequestedPayload,
    scope: TrustScope,
    card: ApprovalCard,
    chip: HTMLElement,
    status: HTMLElement
  ): Promise<void> {
    status.textContent = "Adding runtime trust...";
    if (card.trustButton !== undefined) {
      card.trustButton.disabled = true;
    }
    const response = await this.postJSON("/trust/runtime", {
      session_id: payload.session_id,
      tool: payload.tool,
      path: scope.pattern,
      agent: payload.agent,
      expires: "5m"
    });
    if (!response.ok) {
      status.textContent = await response.text();
      if (card.trustButton !== undefined) {
        card.trustButton.disabled = false;
      }
      return;
    }
    status.textContent = "Runtime trust active.";
    this.startTrustCountdown(card, chip, Date.now() + 5 * 60 * 1000);
  }

  private startTrustCountdown(card: ApprovalCard, chip: HTMLElement, expiresAt: number): void {
    if (card.trustTimer !== undefined) {
      window.clearInterval(card.trustTimer);
    }
    const tick = () => {
      const remaining = expiresAt - Date.now();
      if (remaining <= 0) {
        chip.hidden = true;
        if (card.trustButton !== undefined) {
          card.trustButton.disabled = false;
        }
        if (card.trustTimer !== undefined) {
          window.clearInterval(card.trustTimer);
          card.trustTimer = undefined;
        }
        return;
      }
      chip.hidden = false;
      chip.textContent = `auto-approve ${formatRemaining(remaining)}`;
    };
    tick();
    card.trustTimer = window.setInterval(tick, 1000);
  }
}

function budgetWarningNode(payload: ApprovalRequestedPayload): HTMLElement | undefined {
  const warning = payload.budget_warning;
  if (warning === undefined) {
    return undefined;
  }
  const box = document.createElement("div");
  box.className = "approval-budget";
  const title = document.createElement("div");
  title.className = "approval-budget-title";
  title.textContent = warning.message || "Budget warning";
  const meta = document.createElement("div");
  meta.className = "approval-budget-meta";
  meta.textContent = `${warning.scope} ${formatTokens(warning.projected_tokens)} / ${formatTokens(warning.limit_tokens)} tokens; overrun: ${warning.on_overrun}`;
  box.append(title, meta);
  return box;
}

let diff2htmlUILoad: Promise<Diff2HtmlUIModule> | undefined;

function loadDiff2HtmlUI(): Promise<Diff2HtmlUIModule> {
  if (diff2htmlUILoad === undefined) {
    diff2htmlUILoad = import("diff2html/lib/ui/js/diff2html-ui-slim.js");
  }
  return diff2htmlUILoad;
}

function prepareUnifiedDiff(target: HTMLElement, diff: string): void {
  const lines = lineCount(diff);
  if (lines > maxInlineDiffLines || byteCount(diff) > maxInlineDiffBytes) {
    const summary = document.createElement("div");
    summary.className = "approval-diff-summary";
    summary.textContent = `${lines} line diff`;
    const show = button(`Show more (${lines} lines)`, "secondary");
    show.addEventListener(
      "click",
      () => {
        target.textContent = "Loading diff...";
        void renderUnifiedDiff(target, diff);
      },
      { once: true }
    );
    target.replaceChildren(summary, show);
    return;
  }
  target.textContent = "Loading diff...";
  void renderUnifiedDiff(target, diff);
}

async function renderUnifiedDiff(target: HTMLElement, diff: string): Promise<void> {
  try {
    const { Diff2HtmlUI } = await loadDiff2HtmlUI();
    if (!target.isConnected) {
      return;
    }
    target.replaceChildren();
    const ui = new Diff2HtmlUI(target, diff, {
      drawFileList: false,
      matching: "lines",
      outputFormat: diffOutputFormat()
    });
    ui.draw();
  } catch {
    target.replaceChildren(...lineNodes(diff));
  }
}

function diffOutputFormat(): "line-by-line" | "side-by-side" {
  return window.matchMedia("(orientation: landscape)").matches ? "side-by-side" : "line-by-line";
}

function lineCount(value: string): number {
  if (value === "") {
    return 0;
  }
  return value.split(/\r\n|\r|\n/).length;
}

function byteCount(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function runtimeTrustScope(filePath: string | undefined): TrustScope | undefined {
  const raw = filePath?.trim().replace(/\\/g, "/") ?? "";
  if (raw === "") {
    return undefined;
  }
  const trimmed = raw.replace(/\/+$/, "");
  if (trimmed === "") {
    return undefined;
  }
  const dir = raw.endsWith("/") ? trimmed : parentDir(trimmed);
  if (dir === "" || dir === ".") {
    return { label: ".", pattern: "**" };
  }
  return { label: compactDirLabel(dir), pattern: `${dir.replace(/\/+$/, "")}/**` };
}

function parentDir(path: string): string {
  const idx = path.lastIndexOf("/");
  if (idx < 0) {
    return "";
  }
  if (idx === 0) {
    return "/";
  }
  return path.slice(0, idx);
}

function compactDirLabel(dir: string): string {
  if (dir.length <= 32) {
    return dir;
  }
  const parts = dir.split("/").filter((part) => part !== "");
  return parts.length <= 2 ? dir : `.../${parts.slice(-2).join("/")}`;
}

function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

function formatTokens(value: number): string {
  return Math.max(0, Math.round(value)).toLocaleString("en-US");
}

async function defaultPostJSON(path: string, body: Record<string, string>): Promise<Response> {
  return fetch(path, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
}

function lineNodes(value: string): HTMLElement[] {
  const lines = value.split(/\r?\n/);
  return lines.map((line, idx) => {
    const row = document.createElement("div");
    row.className = "approval-line";
    const number = document.createElement("span");
    number.className = "approval-line-no";
    number.textContent = String(idx + 1);
    const text = document.createElement("code");
    text.textContent = line.length === 0 ? " " : line;
    row.append(number, text);
    return row;
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
