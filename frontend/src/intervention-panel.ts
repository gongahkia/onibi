type CommandState = "pending" | "dispatched" | "succeeded" | "failed" | "timed_out";

type CommandResponse = {
  ok: boolean;
  command_id: string;
  state: CommandState;
  result?: string;
};

type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;
type GetJSON = <T>(path: string) => Promise<T>;

export class InterventionPanel {
  readonly element: HTMLButtonElement;
  private readonly panel: HTMLElement;
  private readonly status: HTMLElement;
  private readonly input: HTMLInputElement;
  private readonly send: HTMLButtonElement;
  private readonly interrupt: HTMLButtonElement;
  private readonly handoffMac: HTMLButtonElement;
  private readonly handoffPhone: HTMLButtonElement;
  private readonly kill: HTMLButtonElement;
  private readonly check: HTMLButtonElement;
  private pending: { id: string; label: string; target?: "mac" | "phone" } | undefined;
  private armedKill = false;

  constructor(
    root: HTMLElement,
    private readonly sessionID: string,
    private readonly postJSON: PostJSON,
    private readonly getJSON: GetJSON,
    private readonly focusTerminal: () => void,
    private readonly handoverConfirmed?: (target: "mac" | "phone") => void
  ) {
    this.element = document.createElement("button");
    this.element.type = "button";
    this.element.className = "control-button intervention-open";
    this.element.textContent = "Act";
    this.element.setAttribute("aria-haspopup", "dialog");
    this.element.setAttribute("aria-expanded", "false");

    this.panel = document.createElement("section");
    this.panel.className = "intervention-panel";
    this.panel.hidden = true;
    this.panel.setAttribute("role", "dialog");
    this.panel.setAttribute("aria-label", "session interventions");
    const heading = document.createElement("h2");
    heading.textContent = "Intervene";
    const close = button("Close", "secondary");
    close.addEventListener("click", () => this.close());
    const header = document.createElement("header");
    header.className = "intervention-header";
    header.append(heading, close);

    const form = document.createElement("form");
    form.className = "intervention-input";
    const label = document.createElement("label");
    label.htmlFor = "intervention-input";
    label.textContent = "Short input";
    this.input = document.createElement("input");
    this.input.id = "intervention-input";
    this.input.maxLength = 240;
    this.input.placeholder = "send one short command";
    this.send = button("Send", "primary");
    this.send.type = "submit";
    form.append(label, this.input, this.send);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      const input = this.input.value.trim();
      if (input === "") {
        this.setStatus("Enter short input first.");
        return;
      }
      void this.submit("input", "Short input", { input });
    });

    const actions = document.createElement("div");
    actions.className = "intervention-actions";
    this.interrupt = button("Interrupt", "danger");
    this.handoffMac = button("Handoff Mac", "secondary");
    this.handoffPhone = button("Handoff Phone", "secondary");
    this.kill = button("Kill", "danger");
    this.check = button("Check status", "secondary");
    this.check.hidden = true;
    this.interrupt.addEventListener("click", () => void this.submit("interrupt", "Interrupt"));
    this.handoffMac.addEventListener(
      "click",
      () => void this.submit("handover", "Handoff to Mac", { target: "mac" })
    );
    this.handoffPhone.addEventListener(
      "click",
      () => void this.submit("handover", "Handoff to phone", { target: "phone" })
    );
    this.kill.addEventListener("click", () => this.requestKill());
    this.check.addEventListener("click", () => void this.checkStatus());
    actions.append(this.interrupt, this.handoffMac, this.handoffPhone, this.kill, this.check);

    this.status = document.createElement("div");
    this.status.className = "intervention-status";
    this.status.setAttribute("role", "status");
    this.panel.append(header, form, actions, this.status);
    root.append(this.panel);
    this.element.addEventListener("click", () => this.open());
  }

  private open(): void {
    this.panel.hidden = false;
    this.element.setAttribute("aria-expanded", "true");
    this.input.focus();
  }

  private close(): void {
    this.panel.hidden = true;
    this.element.setAttribute("aria-expanded", "false");
    this.focusTerminal();
  }

  private requestKill(): void {
    if (!this.armedKill) {
      this.armedKill = true;
      this.kill.textContent = "Confirm kill";
      this.setStatus("Tap Kill again to request SIGKILL.");
      return;
    }
    this.armedKill = false;
    this.kill.textContent = "Kill";
    void this.submit("kill", "Kill");
  }

  private async submit(
    action: "interrupt" | "input" | "handover" | "kill",
    label: string,
    extra: { input?: string; target?: "mac" | "phone" } = {}
  ): Promise<void> {
    this.pending = undefined;
    this.check.hidden = true;
    this.setBusy(true);
    this.setStatus(`Sending ${label.toLowerCase()}...`);
    try {
      const response = await this.postJSON("/control", {
        session_id: this.sessionID,
        action,
        ...extra
      });
      const command = await responseJSON(response);
      if (!response.ok || command === undefined) {
        throw new Error(await responseMessage(response, command));
      }
      this.handleCommand(command, label, extra.target);
    } catch (error) {
      this.setStatus(error instanceof Error ? error.message : "Control unavailable. Retry.");
    } finally {
      this.setBusy(false);
    }
  }

  private async checkStatus(): Promise<void> {
    if (this.pending === undefined) {
      return;
    }
    const pending = this.pending;
    this.setBusy(true);
    this.setStatus("Checking command status...");
    try {
      const command = await this.getJSON<CommandResponse>(
        `/control/${encodeURIComponent(pending.id)}`
      );
      this.handleCommand(command, pending.label, pending.target);
    } catch {
      this.setStatus("Status unavailable. Check connection.");
    } finally {
      this.setBusy(false);
    }
  }

  private handleCommand(
    command: CommandResponse,
    label: string,
    target: "mac" | "phone" | undefined
  ): void {
    if (command.state === "succeeded" && command.ok) {
      this.pending = undefined;
      this.check.hidden = true;
      if (label === "Short input") {
        this.input.value = "";
      }
      this.setStatus(command.result?.trim() || `${label} confirmed.`);
      if (target !== undefined) {
        this.handoverConfirmed?.(target);
      }
      return;
    }
    if (command.state === "failed" || command.state === "timed_out" || !command.ok) {
      this.pending = undefined;
      this.check.hidden = true;
      this.setStatus(command.result?.trim() || `${label} was not confirmed.`);
      return;
    }
    if (command.command_id.trim() === "") {
      this.setStatus("Command acknowledgement missing. Retry.");
      return;
    }
    this.pending = { id: command.command_id, label, target };
    this.check.hidden = false;
    this.setStatus(`${label} awaits host acknowledgement.`);
  }

  private setBusy(busy: boolean): void {
    this.send.disabled = busy;
    this.interrupt.disabled = busy;
    this.handoffMac.disabled = busy;
    this.handoffPhone.disabled = busy;
    this.kill.disabled = busy;
    this.check.disabled = busy;
  }

  private setStatus(text: string): void {
    this.status.textContent = text;
  }
}

async function responseJSON(response: Response): Promise<CommandResponse | undefined> {
  try {
    return (await response.clone().json()) as CommandResponse;
  } catch {
    return undefined;
  }
}

async function responseMessage(
  response: Response,
  command: CommandResponse | undefined
): Promise<string> {
  if (typeof command?.result === "string" && command.result.trim() !== "") {
    return command.result;
  }
  try {
    const body = (await response.clone().json()) as { message?: unknown };
    if (typeof body.message === "string" && body.message.trim() !== "") {
      return body.message;
    }
  } catch {
    return "Control unavailable. Retry.";
  }
  return "Control unavailable. Retry.";
}

function button(label: string, variant: "primary" | "secondary" | "danger"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `intervention-button ${variant}`;
  el.textContent = label;
  return el;
}
