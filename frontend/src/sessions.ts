import type { EventEnvelope } from "./events";

export type SessionCostPayload = {
  session_id: string;
  model?: string;
  input_tokens: number;
  output_tokens: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  daily_tokens: number;
  cost_known: boolean;
  total_micro_cents?: number;
  total_usd?: number;
  updated_at?: string;
};

type FetchJSON = <T>(path: string) => Promise<T>;

export class SessionsPanel {
  private readonly sessions = new Map<string, SessionCostPayload | undefined>();

  constructor(
    private readonly root: HTMLElement,
    private readonly currentSessionID: string,
    private readonly fetchJSON: FetchJSON
  ) {
    this.addSession(currentSessionID);
    void this.loadCost(currentSessionID);
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type === "session.started") {
      const payload = envelope.payload as { session_id?: string };
      if (payload.session_id !== undefined && payload.session_id !== "") {
        this.addSession(payload.session_id);
        void this.loadCost(payload.session_id);
      }
      return;
    }
    if (envelope.type === "cost.updated") {
      const payload = envelope.payload as { session_id?: string };
      if (payload.session_id !== undefined && payload.session_id !== "") {
        this.addSession(payload.session_id);
        void this.loadCost(payload.session_id);
      }
    }
  }

  private addSession(id: string): void {
    if (!this.sessions.has(id)) {
      this.sessions.set(id, undefined);
      this.render();
    }
  }

  private async loadCost(id: string): Promise<void> {
    try {
      const cost = await this.fetchJSON<SessionCostPayload>(`/sessions/${encodeURIComponent(id)}/cost`);
      this.sessions.set(id, cost);
      this.render();
    } catch {
      this.render();
    }
  }

  private render(): void {
    const current = this.sessions.get(this.currentSessionID);
    const header = document.createElement("div");
    header.className = "sessions-current";
    header.textContent = `daily ${formatTokens(current?.daily_tokens ?? 0)}`;

    const list = document.createElement("div");
    list.className = "sessions-list";
    for (const [id, cost] of this.sessions) {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = id === this.currentSessionID ? "session-tab active" : "session-tab";
      tab.title = id;
      const name = document.createElement("span");
      name.className = "session-name";
      name.textContent = shortID(id);
      const value = document.createElement("span");
      value.className = "session-cost";
      value.textContent = formatCost(cost);
      tab.append(name, value);
      tab.addEventListener("click", () => {
        if (id !== this.currentSessionID) {
          window.location.href = `/?session_id=${encodeURIComponent(id)}`;
        }
      });
      list.append(tab);
    }
    this.root.hidden = false;
    this.root.replaceChildren(header, list);
  }
}

function formatCost(cost: SessionCostPayload | undefined): string {
  if (cost === undefined) {
    return "0 tok";
  }
  if (cost.cost_known && cost.total_usd !== undefined) {
    const digits = cost.total_usd > 0 && cost.total_usd < 0.01 ? 4 : 2;
    return `$${cost.total_usd.toFixed(digits)}`;
  }
  return `${formatTokens(cost.total_tokens)} tok`;
}

function formatTokens(value: number): string {
  return `${Math.max(0, Math.round(value)).toLocaleString("en-US")} tok`;
}

function shortID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}
