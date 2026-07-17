import type { ApprovalDecidedPayload, ApprovalRequestedPayload, EventEnvelope } from "./events";
import type { ApprovalWakeLock } from "./wake-lock";

type Diff2HtmlUIModule = typeof import("diff2html/lib/ui/js/diff2html-ui-slim.js");

const maxInlineDiffLines = 200;
const maxInlineDiffBytes = 50 * 1024;

type ApprovalCard = {
  payload: ApprovalRequestedPayload;
  element: HTMLElement;
  approveUntil: number;
};

type PostJSON = (path: string, body: Record<string, string>) => Promise<Response>;

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
    const target = document.createElement("div");
    target.className = "approval-target";
    target.textContent = `target: ${payload.file_path || "not reported"}`;
    const meta = document.createElement("div");
    meta.className = "approval-meta";
    meta.textContent = `${payload.agent} / ${payload.session_id}`;
    const badge = document.createElement("div");
    badge.className = "approval-risk";
    badge.textContent = payload.risk_level;
    header.append(title, target, meta, badge);

    const input = document.createElement("details");
    input.className = "approval-input";
    const summary = document.createElement("summary");
    summary.textContent = inputSummary(payload);
    input.append(summary);
    const evidence = document.createElement("div");
    if (payload.unified_diff !== undefined && payload.unified_diff !== "") {
      evidence.className = "approval-diff";
      prepareUnifiedDiff(evidence, payload.unified_diff);
    } else {
      evidence.append(...lineNodes(payload.scrubbed_input));
    }
    input.append(evidence);
    const actions = document.createElement("div");
    actions.className = "approval-actions";
    const approve = button(
      payload.risk_level === "high" ? "Approve (tap twice)" : "Approve",
      "primary"
    );
    const deny = button("Deny", "danger");
    const edit = button("Edit", "secondary");
    actions.append(approve, deny, edit);

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
    card.append(header, input, actions, editPane, status);
    this.root.append(card);
    window.setTimeout(() => card.scrollIntoView({ block: "nearest", inline: "nearest" }), 50);
    const tracked: ApprovalCard = { payload, element: card, approveUntil: 0 };
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
    let response: Response;
    try {
      response = await this.postJSON(`/approval/${encodeURIComponent(id)}`, body);
    } catch {
      status.textContent = "Connection failed. Retry.";
      return;
    }
    if (!response.ok) {
      status.textContent = await response.text();
      return;
    }
    status.textContent = "Done.";
  }
}

function inputSummary(payload: ApprovalRequestedPayload): string {
  if (payload.unified_diff !== undefined && payload.unified_diff !== "") {
    return `${lineCount(payload.unified_diff)} line diff`;
  }
  const lines = lineCount(payload.scrubbed_input);
  const first = payload.scrubbed_input.split(/\r?\n/, 1)[0].trim();
  return first === ""
    ? `${lines} line input`
    : `${first.slice(0, 96)}${first.length > 96 ? "…" : ""}`;
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
