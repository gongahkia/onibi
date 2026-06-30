import type { EventEnvelope } from "./events";

export type TimelineEntry = {
  kind: string;
  session_id?: string;
  provider_session_id?: string;
  agent?: string;
  turn?: number;
  role?: string;
  tool_name?: string;
  tool_id?: string;
  model?: string;
  summary?: string;
  ts?: string;
  offset?: number;
  input_tokens?: number;
  output_tokens?: number;
  total_input_tokens?: number;
  total_output_tokens?: number;
  payload?: Record<string, unknown>;
};

type Item = {
  entry: TimelineEntry;
  key: string;
};

const maxItems = 500;

export class TimelinePanel {
  private items: Item[] = [];
  private seen = new Set<string>();
  private open = false;

  constructor(
    private readonly root: HTMLElement,
    private readonly currentSessionID: string
  ) {}

  toggle(): void {
    this.open = !this.open;
    this.render();
  }

  handleEnvelope(envelope: EventEnvelope): void {
    if (envelope.type !== "timeline.entry") {
      return;
    }
    const entry = envelope.payload as TimelineEntry;
    if (entry.session_id !== undefined && entry.session_id !== "" && entry.session_id !== this.currentSessionID) {
      return;
    }
    const key = entryKey(entry);
    if (this.seen.has(key)) {
      return;
    }
    this.items.push({ entry, key });
    if (this.items.length > maxItems) {
      this.items = this.items.slice(this.items.length - maxItems);
      this.seen = new Set(this.items.map((item) => item.key));
    }
    this.seen.add(key);
    if (this.open) {
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
    header.className = "timeline-header";
    const title = document.createElement("div");
    title.className = "timeline-title";
    title.textContent = "timeline";
    const count = document.createElement("div");
    count.className = "timeline-count";
    count.textContent = String(this.items.length);
    const close = button("Close");
    close.addEventListener("click", () => {
      this.open = false;
      this.render();
    });
    header.append(title, count, close);

    const list = document.createElement("div");
    list.className = "timeline-list";
    if (this.items.length === 0) {
      const empty = document.createElement("div");
      empty.className = "timeline-empty";
      empty.textContent = "no events";
      list.append(empty);
    } else {
      for (const item of orderedItems(this.items)) {
        list.append(entryNode(item.entry));
      }
    }
    this.root.hidden = false;
    this.root.replaceChildren(header, list);
  }
}

function entryNode(entry: TimelineEntry): HTMLElement {
  const details = document.createElement("details");
  details.className = `timeline-entry kind-${entry.kind}`;
  const summary = document.createElement("summary");
  summary.className = "timeline-summary";
  const icon = document.createElement("span");
  icon.className = "timeline-icon";
  icon.textContent = kindIcon(entry.kind);
  const main = document.createElement("span");
  main.className = "timeline-main";
  const title = document.createElement("span");
  title.className = "timeline-entry-title";
  title.textContent = entryTitle(entry);
  const meta = document.createElement("span");
  meta.className = "timeline-entry-meta";
  meta.textContent = entryMeta(entry);
  main.append(title, meta);
  const when = document.createElement("span");
  when.className = "timeline-entry-time";
  when.textContent = formatWhen(entry.ts);
  summary.append(icon, main, when);
  const payload = document.createElement("pre");
  payload.className = "timeline-payload";
  payload.textContent = JSON.stringify(entry, null, 2);
  details.append(summary, payload);
  return details;
}

function orderedItems(items: Item[]): Item[] {
  return [...items].sort((a, b) => {
    const at = entryTime(a.entry);
    const bt = entryTime(b.entry);
    if (at !== bt) {
      return at - bt;
    }
    return (a.entry.offset ?? 0) - (b.entry.offset ?? 0);
  });
}

function entryTitle(entry: TimelineEntry): string {
  return entry.summary || entry.tool_name || entry.role || entry.kind;
}

function entryMeta(entry: TimelineEntry): string {
  const parts = [entry.kind];
  if (entry.turn !== undefined && entry.turn > 0) {
    parts.push(`turn ${entry.turn}`);
  }
  if (entry.tool_name !== undefined && entry.tool_name !== "") {
    parts.push(entry.tool_name);
  }
  if (entry.kind === "cost") {
    parts.push(`${entry.input_tokens ?? 0}/${entry.output_tokens ?? 0} tok`);
  }
  return parts.join(" / ");
}

function kindIcon(kind: string): string {
  switch (kind) {
    case "turn":
      return "T";
    case "tool_call":
      return ">";
    case "tool_result":
      return "<";
    case "approval":
      return "!";
    case "anomaly":
      return "#";
    case "snapshot":
      return "S";
    case "cost":
      return "$";
    default:
      return "*";
  }
}

function entryKey(entry: TimelineEntry): string {
  return [entry.session_id ?? "", entry.kind, entry.turn ?? 0, entry.offset ?? 0, entry.ts ?? "", entry.summary ?? ""].join("|");
}

function entryTime(entry: TimelineEntry): number {
  const ts = Date.parse(entry.ts ?? "");
  return Number.isFinite(ts) ? ts : 0;
}

function formatWhen(raw: string | undefined): string {
  const ts = Date.parse(raw ?? "");
  if (!Number.isFinite(ts)) {
    return "";
  }
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function button(label: string): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = "approval-button secondary";
  el.textContent = label;
  return el;
}
