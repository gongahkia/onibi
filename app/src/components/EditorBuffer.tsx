import { useCallback, useEffect, useState } from "react";
import { readWorkspaceFile, useSessionStore, writeWorkspaceFile } from "../lib/sessions";

export interface EditorBufferProps {
  path: string;
  workspaceRoot: string;
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

export function EditorBuffer({ path, workspaceRoot }: EditorBufferProps) {
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
        <textarea
          className="editor-textarea"
          aria-label="Editor buffer"
          wrap="off"
          spellCheck={false}
          value={content}
          onChange={(event) => setContent(event.target.value)}
        />
      ) : null}
    </section>
  );
}
