import "asciinema-player/dist/bundle/asciinema-player.css";
import type { Options, Player, Source } from "asciinema-player";

export type RecordingItem = {
  id: string;
  session_id: string;
  name: string;
  created_at: string;
  duration_seconds: number;
  size_bytes: number;
  url: string;
};

type RecordingListResponse = {
  recordings: RecordingItem[];
};

type FetchJSON = <T>(path: string) => Promise<T>;
type FetchText = (path: string) => Promise<string>;
type CreatePlayer = (src: Source, container: HTMLElement, options?: Options) => Player;
type LoadPlayer = () => Promise<CreatePlayer>;
type Speed = 1 | 2 | 4;

export class RecordingPlayerPanel {
  private items: RecordingItem[] = [];
  private open = false;
  private loading = false;
  private status = "";
  private current: RecordingItem | undefined;
  private currentCast = "";
  private player: Player | undefined;
  private playerHost: HTMLElement | undefined;
  private speed: Speed = 1;
  private paused = false;

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly fetchText: FetchText,
    private readonly toast: (message: string) => void,
    private readonly loadPlayer: LoadPlayer = defaultLoadPlayer
  ) {}

  toggle(): void {
    this.open = !this.open;
    if (!this.open) {
      this.closePlayer();
    }
    this.render();
    if (this.open) {
      void this.refresh();
    }
  }

  private async refresh(): Promise<void> {
    this.loading = true;
    this.status = "";
    this.render();
    try {
      const response = await this.fetchJSON<RecordingListResponse>("/recordings");
      this.items = response.recordings;
    } catch {
      this.status = "recordings unavailable";
    } finally {
      this.loading = false;
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
    header.className = "recordings-header";
    const title = document.createElement("div");
    title.className = "recordings-title";
    title.textContent = "recordings";
    const reload = button("Reload", "secondary");
    reload.addEventListener("click", () => void this.refresh());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => {
      this.open = false;
      this.closePlayer();
      this.render();
    });
    header.append(title, reload, close);

    const body = document.createElement("div");
    body.className = "recordings-list";
    if (this.loading) {
      body.append(emptyRow("loading"));
    } else if (this.items.length === 0) {
      body.append(emptyRow(this.status || "no recordings"));
    } else {
      for (const item of this.items) {
        body.append(this.recordingRow(item));
      }
    }
    const status = document.createElement("div");
    status.className = "recordings-status";
    status.textContent = this.status;
    const nodes: HTMLElement[] = [header, body, status];
    if (this.current !== undefined) {
      nodes.push(this.playerOverlay(this.current));
    }
    this.root.hidden = false;
    this.root.replaceChildren(...nodes);
  }

  private recordingRow(item: RecordingItem): HTMLButtonElement {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "recording-row";
    row.title = item.name;
    const name = document.createElement("span");
    name.className = "recording-name";
    name.textContent = item.session_id || item.name;
    const meta = document.createElement("span");
    meta.className = "recording-meta";
    meta.textContent = `${formatWhen(item.created_at)} / ${formatDuration(item.duration_seconds)}`;
    const size = document.createElement("span");
    size.className = "recording-size";
    size.textContent = formatBytes(item.size_bytes);
    row.append(name, meta, size);
    row.addEventListener("click", () => void this.openRecording(item));
    return row;
  }

  private playerOverlay(item: RecordingItem): HTMLElement {
    const overlay = document.createElement("div");
    overlay.className = "recording-player";
    const header = document.createElement("div");
    header.className = "recording-player-header";
    const title = document.createElement("div");
    title.className = "recording-player-title";
    title.textContent = item.session_id || item.name;
    const speed = document.createElement("select");
    speed.className = "recording-speed";
    speed.tabIndex = -1;
    for (const value of [1, 2, 4] as const) {
      const option = document.createElement("option");
      option.value = String(value);
      option.textContent = `${value}x`;
      speed.append(option);
    }
    speed.value = String(this.speed);
    speed.addEventListener("change", () => {
      this.speed = speedValue(speed.value);
      void this.mountPlayer();
    });
    const pause = button(this.paused ? "Play" : "Pause", "secondary");
    pause.addEventListener("click", () => void this.togglePlayback(pause));
    const copy = button("Copy transcript", "secondary");
    copy.addEventListener("click", () => void this.copyTranscript());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => {
      this.closePlayer();
      this.render();
    });
    header.append(title, speed, pause, copy, close);
    this.playerHost = document.createElement("div");
    this.playerHost.className = "recording-player-host";
    overlay.append(header, this.playerHost);
    return overlay;
  }

  private async openRecording(item: RecordingItem): Promise<void> {
    this.current = item;
    this.currentCast = "";
    this.speed = 1;
    this.paused = false;
    this.status = `loading ${item.name}`;
    this.render();
    try {
      this.currentCast = await this.fetchText(item.url);
      this.status = "";
      this.render();
      await this.mountPlayer();
    } catch (err) {
      this.status = err instanceof Error ? err.message : "recording unavailable";
      this.render();
    }
  }

  private async mountPlayer(): Promise<void> {
    if (this.currentCast === "" || this.playerHost === undefined) {
      return;
    }
    this.player?.dispose();
    this.playerHost.replaceChildren();
    const create = await this.loadPlayer();
    this.player = create({ data: this.currentCast, parser: "asciicast" }, this.playerHost, {
      autoPlay: true,
      controls: true,
      fit: "both",
      idleTimeLimit: 2,
      speed: this.speed,
      terminalFontSize: "small"
    });
    this.paused = false;
  }

  private async togglePlayback(buttonEl: HTMLButtonElement): Promise<void> {
    if (this.player === undefined) {
      return;
    }
    if (this.paused) {
      await this.player.play();
      this.paused = false;
      buttonEl.textContent = "Pause";
      return;
    }
    await this.player.pause();
    this.paused = true;
    buttonEl.textContent = "Play";
  }

  private async copyTranscript(): Promise<void> {
    const text = transcriptFromCast(this.currentCast);
    if (text === "") {
      this.toast("Transcript empty.");
      return;
    }
    if (navigator.clipboard?.writeText === undefined) {
      this.toast("Clipboard unavailable.");
      return;
    }
    await navigator.clipboard.writeText(text);
    this.toast("Transcript copied.");
  }

  private closePlayer(): void {
    this.player?.dispose();
    this.player = undefined;
    this.playerHost = undefined;
    this.current = undefined;
    this.currentCast = "";
    this.status = "";
    this.paused = false;
  }
}

export function transcriptFromCast(cast: string): string {
  let out = "";
  for (const line of cast.split(/\n+/)) {
    if (line.trim() === "") {
      continue;
    }
    let event: unknown;
    try {
      event = JSON.parse(line);
    } catch {
      continue;
    }
    if (!Array.isArray(event) || event[1] !== "o" || typeof event[2] !== "string") {
      continue;
    }
    out += event[2];
  }
  return stripControls(out).replace(/\r\n?/g, "\n").trim();
}

async function defaultLoadPlayer(): Promise<CreatePlayer> {
  const mod = await import("asciinema-player");
  return mod.create;
}

function button(label: string, variant: "primary" | "secondary"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${variant}`;
  el.textContent = label;
  el.tabIndex = -1;
  return el;
}

function emptyRow(message: string): HTMLElement {
  const row = document.createElement("div");
  row.className = "recording-empty";
  row.textContent = message;
  return row;
}

function speedValue(value: string): Speed {
  return value === "4" ? 4 : value === "2" ? 2 : 1;
}

function formatWhen(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "unknown";
  }
  return date.toLocaleString([], {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "0s";
  }
  const total = Math.round(seconds);
  const min = Math.floor(total / 60);
  const sec = total % 60;
  return min > 0 ? `${min}m ${sec}s` : `${sec}s`;
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  const kib = bytes / 1024;
  if (kib < 1024) {
    return `${kib.toFixed(1)} KiB`;
  }
  return `${(kib / 1024).toFixed(1)} MiB`;
}

function stripControls(value: string): string {
  return value
    .replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g, "")
    .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\x1b[()][A-Za-z0-9]/g, "")
    .replace(/[\u0000-\u0008\u000b-\u001a\u001c-\u001f\u007f]/g, "");
}
