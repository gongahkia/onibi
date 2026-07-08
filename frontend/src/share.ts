export type ShareViewer = {
  id: string;
  label: string;
  created_at: string;
  last_seen_at: string;
  expires_at: string;
};

type ShareListResponse = {
  viewers: ShareViewer[];
};

type ShareCreateResponse = {
  url: string;
  qr_png_data: string;
  session_id: string;
  role: "viewer";
  expires_at: string;
  ttl: string;
  max_viewers: number;
};

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

const ttlOptions = [
  ["5m", "5min"],
  ["30m", "30min"],
  ["1h", "1h"],
  ["4h", "4h"],
  ["24h", "24h"]
] as const;

export class SharePanel {
  private modal: HTMLElement | undefined;
  private viewers: ShareViewer[] = [];
  private created: ShareCreateResponse | undefined;
  private loading = false;
  private status = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly sessionID: string,
    private readonly fetchJSON: FetchJSON,
    private readonly postJSON: PostJSON,
    private readonly toast: (message: string) => void
  ) {}

  open(): void {
    this.status = "";
    this.created = undefined;
    this.modal?.remove();
    this.modal = document.createElement("div");
    this.modal.className = "share-modal";
    this.modal.addEventListener("click", (event) => {
      if (event.target === this.modal) {
        this.close();
      }
    });
    this.root.append(this.modal);
    this.render();
    void this.refresh();
  }

  private close(): void {
    this.modal?.remove();
    this.modal = undefined;
  }

  private async refresh(): Promise<void> {
    this.loading = true;
    this.render();
    try {
      const response = await this.fetchJSON<ShareListResponse>(
        `/share?session_id=${encodeURIComponent(this.sessionID)}`
      );
      this.viewers = response.viewers;
      this.status = "";
    } catch {
      this.status = "shares unavailable";
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
    shell.className = "share-form";
    const header = document.createElement("div");
    header.className = "share-header";
    const title = document.createElement("div");
    title.className = "share-title";
    title.textContent = "share read-only";
    const reload = button("Reload", "secondary");
    reload.addEventListener("click", () => void this.refresh());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => this.close());
    header.append(title, reload, close);
    shell.append(header, this.createForm());
    if (this.created !== undefined) {
      shell.append(this.resultView(this.created));
    }
    shell.append(this.viewerList(), this.statusLine());
    this.modal.replaceChildren(shell);
  }

  private createForm(): HTMLFormElement {
    const form = document.createElement("form");
    form.className = "share-controls";
    const ttl = select("TTL", ttlOptions, "30m");
    const viewers = select(
      "Viewers",
      [1, 2, 3, 4, 5].map((n) => [String(n), String(n)] as const),
      "1"
    );
    const create = button("Create", "primary");
    create.type = "submit";
    form.append(field("ttl", ttl), field("viewers", viewers), create);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      void this.create(ttl.value, Number(viewers.value));
    });
    return form;
  }

  private async create(ttl: string, maxViewers: number): Promise<void> {
    this.status = "creating share";
    this.render();
    try {
      const response = await this.postJSON("/share", {
        session_id: this.sessionID,
        ttl,
        max_viewers: maxViewers
      });
      const text = await response.text();
      if (!response.ok) {
        throw new Error(text.trim() || `share ${response.status}`);
      }
      this.created = JSON.parse(text) as ShareCreateResponse;
      this.status = "";
      this.toast("Share link created.");
      await this.refresh();
    } catch (err) {
      this.status = err instanceof Error ? err.message : "share failed";
      this.render();
    }
  }

  private resultView(result: ShareCreateResponse): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "share-result";
    const qr = document.createElement("img");
    qr.className = "share-qr";
    qr.alt = "viewer QR";
    qr.src = result.qr_png_data;
    const urlRow = document.createElement("div");
    urlRow.className = "share-url-row";
    const input = document.createElement("input");
    input.className = "share-url";
    input.readOnly = true;
    input.value = result.url;
    const copy = button("Copy", "secondary");
    copy.addEventListener("click", () => void this.copyURL(result.url, input));
    const meta = document.createElement("div");
    meta.className = "share-meta";
    meta.textContent = `expires ${formatWhen(result.expires_at)} / max ${result.max_viewers}`;
    urlRow.append(input, copy);
    wrap.append(qr, urlRow, meta);
    return wrap;
  }

  private async copyURL(value: string, input: HTMLInputElement): Promise<void> {
    if (navigator.clipboard?.writeText !== undefined) {
      await navigator.clipboard.writeText(value);
      this.toast("Share URL copied.");
      return;
    }
    input.select();
    this.toast("Copy unavailable.");
  }

  private viewerList(): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "share-viewers";
    if (this.loading) {
      wrap.append(empty("loading viewers"));
      return wrap;
    }
    if (this.viewers.length === 0) {
      wrap.append(empty("no active viewers"));
      return wrap;
    }
    for (const viewer of this.viewers) {
      wrap.append(this.viewerRow(viewer));
    }
    return wrap;
  }

  private viewerRow(viewer: ShareViewer): HTMLElement {
    const row = document.createElement("div");
    row.className = "share-viewer-row";
    const main = document.createElement("div");
    main.className = "share-viewer-main";
    const id = document.createElement("div");
    id.className = "share-viewer-id";
    id.textContent = shortID(viewer.id);
    id.title = viewer.id;
    const meta = document.createElement("div");
    meta.className = "share-viewer-meta";
    meta.textContent = `${viewer.label || "viewer"} / expires ${formatWhen(viewer.expires_at)}`;
    main.append(id, meta);
    const revoke = button("Revoke", "danger");
    revoke.addEventListener("click", () => void this.revoke(viewer.id));
    row.append(main, revoke);
    return row;
  }

  private async revoke(viewerID: string): Promise<void> {
    this.status = `revoking ${shortID(viewerID)}`;
    this.render();
    try {
      const response = await this.postJSON("/share/revoke", {
        session_id: this.sessionID,
        viewer_id: viewerID
      });
      if (!response.ok) {
        throw new Error((await response.text()).trim() || `revoke ${response.status}`);
      }
      this.toast("Viewer revoked.");
      await this.refresh();
    } catch (err) {
      this.status = err instanceof Error ? err.message : "revoke failed";
      this.render();
    }
  }

  private statusLine(): HTMLElement {
    const el = document.createElement("div");
    el.className = "share-status";
    el.textContent = this.status;
    return el;
  }
}

function field(label: string, input: HTMLElement): HTMLElement {
  const wrap = document.createElement("label");
  wrap.className = "share-field";
  const text = document.createElement("span");
  text.textContent = label;
  wrap.append(text, input);
  return wrap;
}

function select(
  label: string,
  options: ReadonlyArray<readonly [string, string]>,
  value: string
): HTMLSelectElement {
  const el = document.createElement("select");
  el.className = "share-select";
  el.ariaLabel = label;
  for (const [optionValue, text] of options) {
    const option = document.createElement("option");
    option.value = optionValue;
    option.textContent = text;
    el.append(option);
  }
  el.value = value;
  return el;
}

function button(label: string, variant: "primary" | "secondary" | "danger"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${variant === "danger" ? "danger" : variant}`;
  el.textContent = label;
  el.tabIndex = -1;
  return el;
}

function empty(text: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-empty";
  el.textContent = text;
  return el;
}

function formatWhen(raw: string): string {
  const ts = Date.parse(raw);
  if (!Number.isFinite(ts)) {
    return "-";
  }
  return new Date(ts).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit"
  });
}

function shortID(id: string): string {
  return id.length <= 8 ? id : id.slice(0, 8);
}
