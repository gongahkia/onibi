export type TerminalRecovery = {
  mode: "replay" | "snapshot";
  replay_bytes: number;
};

export class TerminalStatus {
  readonly element: HTMLElement;
  private readonly state: HTMLOutputElement;

  constructor(leaveFleet: () => void) {
    const root = document.createElement("nav");
    root.className = "terminal-status";
    root.setAttribute("aria-label", "terminal status");
    const leave = document.createElement("button");
    leave.type = "button";
    leave.className = "control-button terminal-status-fleet";
    leave.textContent = "Fleet";
    leave.setAttribute("aria-label", "Return to fleet health");
    leave.addEventListener("click", leaveFleet);
    this.state = document.createElement("output");
    this.state.className = "terminal-status-state";
    this.state.setAttribute("aria-live", "polite");
    root.append(leave, this.state);
    this.element = root;
    this.setConnecting();
  }

  setConnecting(): void {
    this.set("connecting", "Connecting terminal...");
  }

  setReconnecting(delay: number): void {
    this.set("reconnecting", `Reconnecting in ${Math.max(0, Math.ceil(delay / 1000))}s...`);
  }

  setDisconnected(): void {
    this.set("disconnected", "Disconnected. Retrying...");
  }

  setConnected(): void {
    this.set("connected", "Terminal live");
  }

  setRecovered(recovery: TerminalRecovery): void {
    this.set(
      "recovered",
      recovery.mode === "snapshot"
        ? "Recovered buffered output"
        : recovery.replay_bytes > 0
          ? "Recovered live output"
          : "Terminal live"
    );
  }

  private set(state: string, text: string): void {
    this.element.dataset.state = state;
    this.state.textContent = text;
  }
}
