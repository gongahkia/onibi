import type { TerminalThemeName } from "./terminal";

type FetchJSON = <T>(path: string) => Promise<T>;

type FileTreeResponse = {
  session_id: string;
  root: string;
  entries: FileTreeEntry[];
  truncated?: boolean;
};

type FileTreeEntry = {
  name: string;
  path: string;
  type: "dir" | "file" | "symlink";
  size?: number;
  children?: FileTreeEntry[];
  truncated?: boolean;
};

type FileContentResponse = {
  session_id: string;
  path: string;
  type: string;
  mime: string;
  size: number;
  binary: boolean;
  content?: string;
};

type CodeToHtmlOptions = {
  lang: string;
  theme: TerminalThemeName;
};

type ShikiHighlighter = {
  codeToHtml(code: string, options: { lang: string; theme: string }): string;
};

type ShikiCoreModule = {
  createHighlighterCore(options: { engine: unknown; themes: unknown[]; langs: unknown[] }): Promise<ShikiHighlighter>;
};

type ShikiEngineModule = {
  createJavaScriptRegexEngine(): unknown;
};

type ShikiLangModule = {
  default: unknown[];
};

type ShikiThemeModule = {
  default: unknown;
};

let highlighterPromise: Promise<ShikiHighlighter> | undefined;

export class FilesPanel {
  private tree: FileTreeResponse | undefined;
  private selectedPath = "";
  private viewerHTML = "";
  private status = "";
  private loadingTree = false;
  private openSeq = 0;

  constructor(
    private readonly root: HTMLElement,
    private readonly sessionID: string,
    private readonly fetchJSON: FetchJSON,
    private readonly getTheme: () => TerminalThemeName
  ) {}

  toggle(): void {
    if (this.root.hidden) {
      this.root.hidden = false;
      if (this.tree === undefined && !this.loadingTree) {
        void this.loadTree();
      } else {
        this.render();
      }
      return;
    }
    this.root.hidden = true;
  }

  async loadTree(): Promise<void> {
    this.loadingTree = true;
    this.status = "loading";
    this.render();
    try {
      this.tree = await this.fetchJSON<FileTreeResponse>(`/files/tree?session=${encodeURIComponent(this.sessionID)}`);
      this.status = this.tree.truncated === true ? "truncated" : "";
    } catch {
      this.status = "files unavailable";
    } finally {
      this.loadingTree = false;
      this.render();
    }
  }

  private async openFile(path: string): Promise<void> {
    const seq = ++this.openSeq;
    this.selectedPath = path;
    this.viewerHTML = `<div class="files-empty">loading</div>`;
    this.render();
    try {
      const file = await this.fetchJSON<FileContentResponse>(`/files/content?session=${encodeURIComponent(this.sessionID)}&path=${encodeURIComponent(path)}`);
      if (seq !== this.openSeq) {
        return;
      }
      if (file.binary) {
        this.viewerHTML = `<div class="files-empty">binary ${escapeHTML(file.mime)} / ${formatBytes(file.size)}</div>`;
      } else {
        this.viewerHTML = await codeToHtml(file.content ?? "", { lang: detectFromExt(file.path), theme: this.getTheme() });
      }
      this.render();
    } catch {
      if (seq === this.openSeq) {
        this.viewerHTML = `<div class="files-empty">file unavailable</div>`;
        this.render();
      }
    }
  }

  private render(): void {
    if (this.root.hidden) {
      return;
    }
    const shell = document.createElement("section");
    shell.className = "files-shell";

    const header = document.createElement("div");
    header.className = "files-header";
    const title = document.createElement("div");
    title.className = "files-title";
    title.textContent = "files";
    const status = document.createElement("div");
    status.className = "files-status";
    status.textContent = this.status;
    const reload = panelButton("Reload", () => void this.loadTree());
    const close = panelButton("Close", () => {
      this.root.hidden = true;
    });
    header.append(title, status, reload, close);

    const body = document.createElement("div");
    body.className = "files-body";
    const tree = document.createElement("div");
    tree.className = "files-tree";
    if (this.loadingTree) {
      tree.append(emptyNode("loading"));
    } else if (this.tree === undefined) {
      tree.append(emptyNode(this.status || "empty"));
    } else if (this.tree.entries.length === 0) {
      tree.append(emptyNode("empty"));
    } else {
      tree.append(...this.treeNodes(this.tree.entries, 0));
    }

    const viewer = document.createElement("div");
    viewer.className = "files-viewer";
    const viewerHead = document.createElement("div");
    viewerHead.className = "files-viewer-head";
    viewerHead.textContent = this.selectedPath || this.tree?.root || "";
    const code = document.createElement("div");
    code.className = "files-code-shell";
    code.innerHTML = this.viewerHTML || `<div class="files-empty">select a file</div>`;
    viewer.append(viewerHead, code);

    body.append(tree, viewer);
    shell.append(header, body);
    this.root.replaceChildren(shell);
  }

  private treeNodes(entries: FileTreeEntry[], depth: number): HTMLElement[] {
    return entries.map((entry) => this.treeNode(entry, depth));
  }

  private treeNode(entry: FileTreeEntry, depth: number): HTMLElement {
    if (entry.type === "dir") {
      const details = document.createElement("details");
      details.className = "files-dir";
      details.open = depth < 2;
      const summary = document.createElement("summary");
      summary.textContent = entry.name + (entry.truncated === true ? " ..." : "");
      details.append(summary, ...this.treeNodes(entry.children ?? [], depth + 1));
      return details;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = entry.path === this.selectedPath ? "files-file active" : "files-file";
    button.textContent = entry.name;
    button.title = entry.path;
    button.addEventListener("click", () => void this.openFile(entry.path));
    return button;
  }
}

export function detectFromExt(path: string): string {
  const ext = path.toLowerCase().split(".").pop() ?? "";
  const map: Record<string, string> = {
    bash: "bash",
    c: "c",
    css: "css",
    go: "go",
    h: "c",
    html: "html",
    js: "javascript",
    json: "json",
    jsx: "jsx",
    md: "markdown",
    mjs: "javascript",
    py: "python",
    rs: "rust",
    sh: "bash",
    swift: "swift",
    ts: "typescript",
    tsx: "tsx",
    txt: "text",
    yaml: "yaml",
    yml: "yaml",
    zig: "zig"
  };
  return map[ext] ?? "text";
}

async function codeToHtml(src: string, opts: CodeToHtmlOptions): Promise<string> {
  try {
    const highlighter = await shikiHighlighter();
    return highlighter.codeToHtml(src, {
      lang: shikiLang(opts.lang),
      theme: shikiTheme(opts.theme)
    });
  } catch {
    return fallbackCodeToHtml(src, opts);
  }
}

function shikiHighlighter(): Promise<ShikiHighlighter> {
  highlighterPromise ??= loadShikiHighlighter();
  return highlighterPromise;
}

async function loadShikiHighlighter(): Promise<ShikiHighlighter> {
  const shiki = (await import("shiki/core")) as ShikiCoreModule;
  const engine = (await import("shiki/engine/javascript")) as ShikiEngineModule;
  const [githubDark, githubLight, catppuccinMocha, tokyoNight, solarizedDark] = await Promise.all([
    import("shiki/themes/github-dark.mjs") as Promise<ShikiThemeModule>,
    import("shiki/themes/github-light.mjs") as Promise<ShikiThemeModule>,
    import("shiki/themes/catppuccin-mocha.mjs") as Promise<ShikiThemeModule>,
    import("shiki/themes/tokyo-night.mjs") as Promise<ShikiThemeModule>,
    import("shiki/themes/solarized-dark.mjs") as Promise<ShikiThemeModule>
  ]);
  const langs = await Promise.all([
    import("shiki/langs/bash.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/c.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/css.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/go.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/html.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/javascript.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/json.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/jsx.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/markdown.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/python.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/rust.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/swift.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/tsx.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/typescript.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/yaml.mjs") as Promise<ShikiLangModule>,
    import("shiki/langs/zig.mjs") as Promise<ShikiLangModule>
  ]);
  return shiki.createHighlighterCore({
    engine: engine.createJavaScriptRegexEngine(),
    themes: [githubDark.default, githubLight.default, catppuccinMocha.default, tokyoNight.default, solarizedDark.default],
    langs: langs.flatMap((lang) => lang.default)
  });
}

function shikiLang(lang: string): string {
  if (lang === "text") {
    return "text";
  }
  return lang;
}

function shikiTheme(theme: TerminalThemeName): string {
  switch (theme) {
    case "catppuccin-mocha":
      return "catppuccin-mocha";
    case "tokyo-night":
      return "tokyo-night";
    case "solarized-dark":
      return "solarized-dark";
    case "light":
      return "github-light";
    default:
      return "github-dark";
  }
}

function fallbackCodeToHtml(src: string, opts: CodeToHtmlOptions): string {
  return `<pre class="files-code" data-lang="${escapeHTML(opts.lang)}" data-code-theme="${escapeHTML(opts.theme)}"><code>${escapeHTML(src)}</code></pre>`;
}

function panelButton(label: string, action: () => void): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "control-button";
  button.textContent = label;
  button.addEventListener("click", action);
  return button;
}

function emptyNode(text: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "files-empty";
  el.textContent = text;
  return el;
}

function formatBytes(size: number): string {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KiB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MiB`;
}

function escapeHTML(value: string): string {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}
