import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from "react";
import { css } from "@codemirror/lang-css";
import { html } from "@codemirror/lang-html";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { python } from "@codemirror/lang-python";
import { rust } from "@codemirror/lang-rust";
import { yaml } from "@codemirror/lang-yaml";
import {
  HighlightStyle,
  syntaxHighlighting,
  type LanguageSupport,
} from "@codemirror/language";
import { EditorState, Prec, type Extension } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import {
  copyLineDown,
  copyLineUp,
  cursorDocEnd,
  cursorDocStart,
  cursorLineEnd,
  cursorLineStart,
  deleteLine,
  emacsStyleKeymap,
  indentLess,
  indentMore,
  insertBlankLine,
  moveLineDown,
  moveLineUp,
  selectLine,
  toggleComment,
} from "@codemirror/commands";
import {
  findNext,
  findPrevious,
  openSearchPanel,
  searchKeymap,
  selectNextOccurrence,
  selectSelectionMatches,
} from "@codemirror/search";
import { tags as highlightTags } from "@lezer/highlight";
import { vim } from "@replit/codemirror-vim";
import { basicSetup } from "codemirror";
import ReactMarkdown from "react-markdown";
import rehypeRaw from "rehype-raw";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";
import {
  readWorkspaceFile,
  readWorkspacePreviewFile,
  type EditorKeybindingMode,
  useSessionStore,
  writeWorkspaceFile,
} from "../lib/sessions";

export interface EditorBufferProps {
  path: string;
  workspaceRoot: string;
  fontFamily?: string;
  keybindingMode?: EditorKeybindingMode;
}

type BufferState = "loading" | "ready" | "preview" | "binary" | "large" | "error";
type PreviewKind = "image" | "pdf" | "audio" | "video";

const PREVIEW_MIME_BY_EXTENSION: Record<string, { kind: PreviewKind; mime: string }> = {
  ico: { kind: "image", mime: "image/x-icon" },
  png: { kind: "image", mime: "image/png" },
  jpg: { kind: "image", mime: "image/jpeg" },
  jpeg: { kind: "image", mime: "image/jpeg" },
  gif: { kind: "image", mime: "image/gif" },
  webp: { kind: "image", mime: "image/webp" },
  svg: { kind: "image", mime: "image/svg+xml" },
  pdf: { kind: "pdf", mime: "application/pdf" },
  mp3: { kind: "audio", mime: "audio/mpeg" },
  wav: { kind: "audio", mime: "audio/wav" },
  m4a: { kind: "audio", mime: "audio/mp4" },
  ogg: { kind: "audio", mime: "audio/ogg" },
  mp4: { kind: "video", mime: "video/mp4" },
  webm: { kind: "video", mime: "video/webm" },
  mov: { kind: "video", mime: "video/quicktime" },
};

function extensionForPath(path: string): string {
  return path.split(".").pop()?.toLowerCase() ?? "";
}

function previewTypeForPath(path: string) {
  return PREVIEW_MIME_BY_EXTENSION[extensionForPath(path)];
}

function isMarkdownPath(path: string): boolean {
  return ["md", "mdx", "markdown"].includes(extensionForPath(path));
}

function isExternalImageSource(src: string): boolean {
  return /^(?:https?:|data:|blob:)/i.test(src);
}

function stripUrlSuffix(src: string): string {
  const queryIndex = src.search(/[?#]/);
  return queryIndex >= 0 ? src.slice(0, queryIndex) : src;
}

function normalizeAbsolutePath(path: string): string {
  const parts: string[] = [];
  for (const part of path.split("/")) {
    if (!part || part === ".") {
      continue;
    }
    if (part === "..") {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return `/${parts.join("/")}`;
}

function dirname(path: string): string {
  const index = path.lastIndexOf("/");
  return index <= 0 ? "/" : path.slice(0, index);
}

function decodePathComponent(path: string): string {
  try {
    return decodeURIComponent(path);
  } catch {
    return path;
  }
}

function resolveMarkdownImagePath(
  src: string | undefined,
  markdownPath: string,
  workspaceRoot: string,
): string | null {
  if (!src || isExternalImageSource(src)) {
    return null;
  }
  const cleanSrc = decodePathComponent(stripUrlSuffix(src.trim()));
  if (!cleanSrc || cleanSrc.startsWith("//")) {
    return null;
  }
  const root = normalizeAbsolutePath(workspaceRoot);
  let candidate: string;
  if (cleanSrc.startsWith("file://")) {
    try {
      candidate = new URL(cleanSrc).pathname;
    } catch {
      return null;
    }
  } else if (cleanSrc.startsWith(root)) {
    candidate = cleanSrc;
  } else if (cleanSrc.startsWith("/")) {
    candidate = `${root}/${cleanSrc.slice(1)}`;
  } else {
    candidate = `${dirname(markdownPath)}/${cleanSrc}`;
  }
  const normalized = normalizeAbsolutePath(candidate);
  if (normalized !== root && !normalized.startsWith(`${root}/`)) {
    return null;
  }
  return previewTypeForPath(normalized)?.kind === "image" ? normalized : null;
}

function bytesToText(bytes: number[]): string | null {
  if (bytes.some((byte) => byte === 0)) {
    return null;
  }
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(new Uint8Array(bytes));
  } catch {
    return null;
  }
}

function isLargeFileError(message: string): boolean {
  return message.toLowerCase().includes("file too large");
}

function languageForPath(path: string): LanguageSupport | [] {
  const extension = extensionForPath(path);
  switch (extension) {
    case "cjs":
    case "js":
    case "mjs":
      return javascript();
    case "jsx":
      return javascript({ jsx: true });
    case "ts":
      return javascript({ typescript: true });
    case "tsx":
      return javascript({ jsx: true, typescript: true });
    case "json":
    case "jsonc":
      return json();
    case "css":
    case "scss":
    case "sass":
      return css();
    case "htm":
    case "html":
    case "svelte":
    case "vue":
      return html();
    case "md":
    case "mdx":
    case "markdown":
      return markdown();
    case "py":
    case "pyw":
      return python();
    case "rs":
      return rust();
    case "yaml":
    case "yml":
      return yaml();
    default:
      return [];
  }
}

const editorTheme = EditorView.theme(
  {
    "&": {
      height: "100%",
      color: "var(--fg-0)",
      backgroundColor: "var(--bg-0)",
      fontSize: "var(--font-size-editor)",
    },
    ".cm-scroller": {
      fontFamily: "var(--font-editor)",
      lineHeight: "1.45",
      overflow: "auto",
    },
    ".cm-content": {
      caretColor: "var(--terminal-cursor)",
      minHeight: "100%",
      padding: "14px 0",
    },
    ".cm-line": {
      padding: "0 14px",
    },
    ".cm-gutters": {
      color: "var(--fg-2)",
      backgroundColor: "var(--bg-1)",
      borderRight: "1px solid var(--border)",
    },
    ".cm-lineNumbers .cm-gutterElement": {
      minWidth: "42px",
      padding: "0 10px 0 8px",
    },
    ".cm-activeLine": {
      backgroundColor: "color-mix(in srgb, var(--accent) 9%, transparent)",
    },
    ".cm-activeLineGutter": {
      color: "var(--fg-0)",
      backgroundColor: "color-mix(in srgb, var(--accent) 12%, var(--bg-1))",
    },
    ".cm-cursor, .cm-dropCursor": {
      borderLeftColor: "var(--terminal-cursor)",
    },
    "&.cm-focused": {
      outline: "none",
    },
    "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": {
      backgroundColor: "var(--terminal-selection)",
    },
    ".cm-searchMatch": {
      backgroundColor: "color-mix(in srgb, var(--flash) 34%, transparent)",
      outline: "1px solid var(--accent)",
    },
  },
);

const editorHighlightStyle = HighlightStyle.define([
  { tag: highlightTags.keyword, color: "var(--accent)" },
  { tag: [highlightTags.name, highlightTags.deleted], color: "var(--fg-0)" },
  {
    tag: [highlightTags.variableName, highlightTags.propertyName],
    color: "var(--fg-0)",
  },
  {
    tag: [highlightTags.function(highlightTags.variableName), highlightTags.labelName],
    color: "var(--accent-2)",
  },
  {
    tag: [highlightTags.string, highlightTags.special(highlightTags.string)],
    color: "#87b379",
  },
  { tag: [highlightTags.number, highlightTags.bool, highlightTags.null], color: "#c792ea" },
  {
    tag: [highlightTags.comment, highlightTags.lineComment, highlightTags.blockComment],
    color: "var(--fg-2)",
    fontStyle: "italic",
  },
  { tag: [highlightTags.atom, highlightTags.meta], color: "#f78c6c" },
  { tag: [highlightTags.typeName, highlightTags.className], color: "#82aaff" },
  { tag: highlightTags.invalid, color: "var(--danger)" },
]);

interface CodeEditorProps {
  value: string;
  path: string;
  fontFamily?: string;
  onChange: (value: string) => void;
  onSave: () => void;
  keybindingMode: EditorKeybindingMode;
  onScroller?: (element: HTMLElement | null) => void;
}

function CodeEditor({
  value,
  path,
  fontFamily,
  onChange,
  onSave,
  keybindingMode,
  onScroller,
}: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);
  useEffect(() => {
    onSaveRef.current = onSave;
  }, [onSave]);

  const language = useMemo(() => languageForPath(path), [path]);

  useEffect(() => {
    const parent = containerRef.current;
    if (!parent) {
      return undefined;
    }
    const extensions: Extension[] = [
      ...(keybindingMode === "vim" ? [vim()] : []),
      Prec.high(
        keymap.of([
          ...(keybindingMode === "emacs" ? emacsStyleKeymap : []),
          ...searchKeymap,
          {
            key: "Mod-s",
            run: () => {
              onSaveRef.current();
              return true;
            },
          },
          { key: "Mod-f", run: openSearchPanel },
          { key: "Mod-g", run: findNext },
          { key: "Shift-Mod-g", run: findPrevious },
          { key: "Mod-d", run: selectNextOccurrence },
          { key: "Shift-Mod-l", run: selectSelectionMatches },
          { key: "Mod-l", run: selectLine },
          { key: "Mod-/", run: toggleComment },
          { key: "Mod-[", run: indentLess },
          { key: "Mod-]", run: indentMore },
          { key: "Alt-Up", run: moveLineUp },
          { key: "Alt-Down", run: moveLineDown },
          { key: "Shift-Alt-Up", run: copyLineUp },
          { key: "Shift-Alt-Down", run: copyLineDown },
          { key: "Mod-Enter", run: insertBlankLine },
          { key: "Mod-Backspace", run: deleteLine },
          { key: "Mod-ArrowLeft", run: cursorLineStart },
          { key: "Mod-ArrowRight", run: cursorLineEnd },
          { key: "Shift-Mod-ArrowLeft", run: cursorDocStart },
          { key: "Shift-Mod-ArrowRight", run: cursorDocEnd },
          { key: "Ctrl-d", run: selectNextOccurrence },
        ]),
      ),
      basicSetup,
      editorTheme,
      syntaxHighlighting(editorHighlightStyle),
      EditorView.contentAttributes.of({
        "aria-label": "Editor buffer",
        spellcheck: "false",
      }),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          onChangeRef.current(update.state.doc.toString());
        }
      }),
      language,
    ];
    const view = new EditorView({
      parent,
      state: EditorState.create({ doc: value, extensions }),
    });
    viewRef.current = view;
    onScroller?.(view.scrollDOM);
    return () => {
      onScroller?.(null);
      view.destroy();
      viewRef.current = null;
    };
  }, [keybindingMode, language, onScroller]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) {
      return;
    }
    const current = view.state.doc.toString();
    if (current !== value) {
      view.dispatch({
        changes: { from: 0, to: current.length, insert: value },
      });
    }
  }, [value]);

  return (
    <div
      ref={containerRef}
      className="editor-code"
      style={
        fontFamily
          ? ({ "--font-editor": fontFamily } as CSSProperties)
          : undefined
      }
    />
  );
}

function useSyncedScroll(
  primary: HTMLElement | null,
  secondary: HTMLElement | null,
  enabled: boolean,
) {
  const syncingRef = useRef(false);

  useEffect(() => {
    if (!enabled || !primary || !secondary) {
      return undefined;
    }

    function sync(source: HTMLElement, target: HTMLElement) {
      if (syncingRef.current) {
        return;
      }
      const sourceRange = source.scrollHeight - source.clientHeight;
      const targetRange = target.scrollHeight - target.clientHeight;
      if (sourceRange <= 0 || targetRange <= 0) {
        return;
      }
      syncingRef.current = true;
      target.scrollTop = (source.scrollTop / sourceRange) * targetRange;
      requestAnimationFrame(() => {
        syncingRef.current = false;
      });
    }

    const syncToSecondary = () => sync(primary, secondary);
    const syncToPrimary = () => sync(secondary, primary);
    primary.addEventListener("scroll", syncToSecondary, { passive: true });
    secondary.addEventListener("scroll", syncToPrimary, { passive: true });
    return () => {
      primary.removeEventListener("scroll", syncToSecondary);
      secondary.removeEventListener("scroll", syncToPrimary);
    };
  }, [enabled, primary, secondary]);
}

function MarkdownPreview({
  content,
  path,
  workspaceRoot,
  onPreview,
}: {
  content: string;
  path: string;
  workspaceRoot: string;
  onPreview: (element: HTMLElement | null) => void;
}) {
  return (
    <article ref={onPreview} className="markdown-preview" aria-label="Markdown preview">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw, rehypeSanitize]}
        components={{
          a: ({ href, children, ...props }) => (
            <a href={href} target="_blank" rel="noreferrer" {...props}>
              {children}
            </a>
          ),
          img: ({ src, alt, ...props }) => (
            <MarkdownImage
              src={typeof src === "string" ? src : undefined}
              alt={alt ?? ""}
              markdownPath={path}
              workspaceRoot={workspaceRoot}
              {...props}
            />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </article>
  );
}

function MarkdownImage({
  src,
  alt,
  markdownPath,
  workspaceRoot,
  ...props
}: {
  src?: string;
  alt?: string;
  markdownPath: string;
  workspaceRoot: string;
}) {
  const [resolvedSrc, setResolvedSrc] = useState(src ?? "");

  useEffect(() => {
    const localPath = resolveMarkdownImagePath(src, markdownPath, workspaceRoot);
    if (!localPath) {
      setResolvedSrc(src ?? "");
      return undefined;
    }
    const previewType = previewTypeForPath(localPath);
    if (!previewType) {
      setResolvedSrc(src ?? "");
      return undefined;
    }
    let cancelled = false;
    let objectUrl: string | null = null;
    setResolvedSrc("");
    void readWorkspacePreviewFile(workspaceRoot, localPath)
      .then((bytes) => {
        if (cancelled) {
          return;
        }
        objectUrl = URL.createObjectURL(
          new Blob([new Uint8Array(bytes)], { type: previewType.mime }),
        );
        setResolvedSrc(objectUrl);
      })
      .catch(() => {
        if (!cancelled) {
          setResolvedSrc(src ?? "");
        }
      });
    return () => {
      cancelled = true;
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
      }
    };
  }, [markdownPath, src, workspaceRoot]);

  return <img src={resolvedSrc} alt={alt ?? src ?? ""} {...props} />;
}

function FilePreview({
  kind,
  url,
  path,
}: {
  kind: PreviewKind;
  url: string;
  path: string;
}) {
  if (kind === "image") {
    return (
      <div className="file-preview image-preview">
        <img src={url} alt={path} />
      </div>
    );
  }
  if (kind === "pdf") {
    return (
      <div className="file-preview pdf-preview">
        <object data={url} type="application/pdf" aria-label={path}>
          <iframe src={url} title={path} />
        </object>
      </div>
    );
  }
  if (kind === "audio") {
    return (
      <div className="file-preview media-preview">
        <audio src={url} controls />
      </div>
    );
  }
  return (
    <div className="file-preview media-preview">
      <video src={url} controls />
    </div>
  );
}

export function EditorBuffer({
  path,
  workspaceRoot,
  fontFamily,
  keybindingMode = "standard",
}: EditorBufferProps) {
  const selectFile = useSessionStore((state) => state.selectFile);
  const [state, setState] = useState<BufferState>("loading");
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [previewKind, setPreviewKind] = useState<PreviewKind | null>(null);
  const [editorScroller, setEditorScroller] = useState<HTMLElement | null>(null);
  const [markdownPreview, setMarkdownPreview] = useState<HTMLElement | null>(null);
  const previewUrlRef = useRef<string | null>(null);
  const dirty = content !== savedContent;
  const isMarkdown = isMarkdownPath(path);

  useSyncedScroll(editorScroller, markdownPreview, state === "ready" && isMarkdown);

  const loadFile = useCallback(async () => {
    setState("loading");
    setError(null);
    if (previewUrlRef.current) {
      URL.revokeObjectURL(previewUrlRef.current);
      previewUrlRef.current = null;
      setPreviewUrl(null);
      setPreviewKind(null);
    }
    try {
      const previewType = previewTypeForPath(path);
      if (previewType) {
        const bytes = await readWorkspacePreviewFile(workspaceRoot, path);
        const url = URL.createObjectURL(
          new Blob([new Uint8Array(bytes)], { type: previewType.mime }),
        );
        previewUrlRef.current = url;
        setPreviewUrl(url);
        setPreviewKind(previewType.kind);
        setState("preview");
        setContent("");
        setSavedContent("");
        return;
      }
      const bytes = await readWorkspaceFile(workspaceRoot, path);
      const text = bytesToText(bytes);
      if (text === null) {
        setState("binary");
        setContent("");
        setSavedContent("");
      } else {
        setState("ready");
        setContent(text);
        setSavedContent(text);
      }
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : String(caught);
      setError(message);
      setState(isLargeFileError(message) ? "large" : "error");
    }
  }, [path, workspaceRoot]);

  useEffect(() => {
    void loadFile();
    return () => {
      if (previewUrlRef.current) {
        URL.revokeObjectURL(previewUrlRef.current);
        previewUrlRef.current = null;
      }
    };
  }, [loadFile]);

  async function save() {
    await writeWorkspaceFile(workspaceRoot, path, new TextEncoder().encode(content));
    setSavedContent(content);
  }

  function close() {
    if (dirty && !window.confirm("Discard unsaved changes?")) {
      return;
    }
    selectFile(null);
  }

  function copyPath() {
    void navigator.clipboard?.writeText(path);
  }

  return (
    <section className="editor-buffer" data-testid="editor-buffer">
      <header className="editor-header">
        <div className="editor-path" title={path}>
          {path}
          {dirty ? <span className="dirty-dot" aria-label="dirty">*</span> : null}
        </div>
        <div className="editor-actions">
          <button
            type="button"
            className="text-button"
            disabled={!dirty || state !== "ready"}
            onClick={() => void save()}
          >
            Save
          </button>
          <button
            type="button"
            className="text-button"
            disabled={state !== "ready"}
            onClick={() => void loadFile()}
          >
            Discard
          </button>
          <button type="button" className="text-button" onClick={close}>
            Close
          </button>
        </div>
      </header>
      {state === "loading" ? <div className="editor-message">Loading file</div> : null}
      {state === "binary" ? (
        <div className="editor-message">Cannot edit binary file.</div>
      ) : null}
      {state === "preview" && previewUrl && previewKind ? (
        <FilePreview kind={previewKind} url={previewUrl} path={path} />
      ) : null}
      {state === "large" ? (
        <div className="editor-message">
          <div>File too large; open in $EDITOR.</div>
          <button type="button" className="text-button" onClick={copyPath}>
            Copy path
          </button>
        </div>
      ) : null}
      {state === "error" ? <div className="editor-error">{error}</div> : null}
      {state === "ready" ? (
        isMarkdown ? (
          <div className="editor-markdown-split">
            <CodeEditor
              path={path}
              fontFamily={fontFamily}
              value={content}
              onChange={setContent}
              onSave={() => void save()}
              keybindingMode={keybindingMode}
              onScroller={setEditorScroller}
            />
            <MarkdownPreview
              content={content}
              path={path}
              workspaceRoot={workspaceRoot}
              onPreview={setMarkdownPreview}
            />
          </div>
        ) : (
          <CodeEditor
            path={path}
            fontFamily={fontFamily}
            value={content}
            onChange={setContent}
            onSave={() => void save()}
            keybindingMode={keybindingMode}
          />
        )
      ) : null}
    </section>
  );
}
