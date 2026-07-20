export type SessionTool = {
  label: string;
  action: () => void;
};

export class SessionToolsPanel {
  readonly element = document.createElement("button");
  private modal: HTMLElement | undefined;

  constructor(
    private readonly root: HTMLElement,
    private readonly tools: SessionTool[]
  ) {
    this.element.type = "button";
    this.element.className = "control-button";
    this.element.textContent = "MORE";
    this.element.setAttribute("aria-haspopup", "dialog");
    this.element.setAttribute("aria-expanded", "false");
    this.element.addEventListener("click", () => this.open());
  }

  private open(): void {
    if (this.modal !== undefined) {
      return;
    }
    const modal = document.createElement("div");
    modal.className = "modal-overlay";
    modal.addEventListener("click", (event) => {
      if (event.target === modal) {
        this.close();
      }
    });
    modal.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        event.preventDefault();
        this.close();
      }
    });
    const sheet = document.createElement("section");
    sheet.className = "modal-sheet session-tools-form";
    sheet.setAttribute("role", "dialog");
    sheet.setAttribute("aria-modal", "true");
    sheet.setAttribute("aria-label", "session tools");
    const header = document.createElement("div");
    header.className = "modal-header";
    const title = document.createElement("div");
    title.className = "modal-title";
    title.textContent = "session tools";
    const close = button("Close");
    close.addEventListener("click", () => this.close());
    header.append(title, close);
    const list = document.createElement("div");
    list.className = "session-tools-list";
    let first: HTMLButtonElement | undefined;
    for (const tool of this.tools) {
      const action = button(tool.label);
      action.classList.add("session-tools-action");
      action.addEventListener("click", () => {
        this.close();
        tool.action();
      });
      first ??= action;
      list.append(action);
    }
    sheet.append(header, list);
    modal.append(sheet);
    this.modal = modal;
    this.root.append(modal);
    this.element.setAttribute("aria-expanded", "true");
    first?.focus();
  }

  private close(): void {
    this.modal?.remove();
    this.modal = undefined;
    this.element.setAttribute("aria-expanded", "false");
    this.element.focus();
  }
}

function button(label: string): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "approval-button secondary";
  el.textContent = label;
  return el;
}
