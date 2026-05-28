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
import { EditorState, type Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { tags as highlightTags } from "@lezer/highlight";
import { basicSetup } from "codemirror";
import { readWorkspaceFile, useSessionStore, writeWorkspaceFile } from "../lib/sessions";

export interface EditorBufferProps {
  path: string;
  workspaceRoot: string;
  fontFamily?: string;
}

type BufferState = "loading" | "ready" | "binary" | "large" | "error";

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
  const extension = path.split(".").pop()?.toLowerCase();
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
}

function CodeEditor({ value, path, fontFamily, onChange }: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  const language = useMemo(() => languageForPath(path), [path]);

  useEffect(() => {
    const parent = containerRef.current;
    if (!parent) {
      return undefined;
    }
    const extensions: Extension[] = [
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
    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, [language]);

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

export function EditorBuffer({ path, workspaceRoot, fontFamily }: EditorBufferProps) {
  const selectFile = useSessionStore((state) => state.selectFile);
  const [state, setState] = useState<BufferState>("loading");
  const [content, setContent] = useState("");
  const [savedContent, setSavedContent] = useState("");
  const [error, setError] = useState<string | null>(null);
  const dirty = content !== savedContent;

  const loadFile = useCallback(async () => {
    setState("loading");
    setError(null);
    try {
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
        <CodeEditor
          path={path}
          fontFamily={fontFamily}
          value={content}
          onChange={setContent}
        />
      ) : null}
    </section>
  );
}
