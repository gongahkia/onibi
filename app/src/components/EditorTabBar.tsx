import { useEffect, useState, type MouseEvent } from "react";
import {
  bufferKey,
  bufferLabel,
  useSessionStore,
  type MainSelection,
} from "../lib/sessions";

function iconForBuffer(buffer: MainSelection): string {
  if (buffer.type === "git-diff") return "codicon-diff";
  if (buffer.type === "agent-review") return "codicon-comment-discussion";
  if (buffer.type === "web") return "codicon-globe";
  return "codicon-file";
}

function suffixForBuffer(buffer: MainSelection): string {
  if (buffer.type === "git-diff") {
    return buffer.stage === "staged" ? " (staged)" : " (working)";
  }
  if (buffer.type === "agent-review") {
    return " (review)";
  }
  return "";
}

interface ContextMenuState {
  x: number;
  y: number;
  key: string;
}

export function EditorTabBar() {
  const openBuffers = useSessionStore((state) => state.openBuffers);
  const activeBufferKey = useSessionStore((state) => state.activeBufferKey);
  const setActiveBuffer = useSessionStore((state) => state.setActiveBuffer);
  const closeBuffer = useSessionStore((state) => state.closeBuffer);
  const closeOtherBuffers = useSessionStore((state) => state.closeOtherBuffers);
  const closeAllBuffers = useSessionStore((state) => state.closeAllBuffers);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);

  useEffect(() => {
    if (!contextMenu) return;
    function close() {
      setContextMenu(null);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") close();
    }
    window.addEventListener("click", close);
    window.addEventListener("scroll", close, true);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("keydown", onKey);
    };
  }, [contextMenu]);

  if (openBuffers.length === 0) {
    return null;
  }

  function handleContextMenu(event: MouseEvent, key: string) {
    event.preventDefault();
    event.stopPropagation();
    setContextMenu({
      x: Math.max(8, Math.min(event.clientX, window.innerWidth - 220)),
      y: Math.max(8, Math.min(event.clientY, window.innerHeight - 160)),
      key,
    });
  }

  function handleAuxClick(event: MouseEvent, key: string) {
    if (event.button === 1) {
      event.preventDefault();
      event.stopPropagation();
      closeBuffer(key);
    }
  }

  return (
    <div className="editor-tab-bar" role="tablist" aria-label="Open files">
      <div className="editor-tabs">
        {openBuffers.map((buffer) => {
          const key = bufferKey(buffer);
          const label = bufferLabel(buffer);
          const isActive = key === activeBufferKey;
          return (
            <div
              key={key}
              role="tab"
              aria-selected={isActive}
              className={`editor-tab ${isActive ? "active" : ""}`}
              title={buffer.type === "web" ? buffer.url : buffer.path}
              onClick={(event) => {
                event.stopPropagation();
                setActiveBuffer(key);
              }}
              onContextMenu={(event) => handleContextMenu(event, key)}
              onAuxClick={(event) => handleAuxClick(event, key)}
            >
              <i
                className={`codicon ${iconForBuffer(buffer)}`}
                aria-hidden="true"
              />
              <span className="editor-tab-label">
                {label}
                <span className="editor-tab-suffix">{suffixForBuffer(buffer)}</span>
              </span>
              <button
                type="button"
                className="editor-tab-close"
                aria-label={`Close ${label}`}
                title="Close"
                onClick={(event) => {
                  event.stopPropagation();
                  closeBuffer(key);
                }}
              >
                <i className="codicon codicon-close" aria-hidden="true" />
              </button>
            </div>
          );
        })}
      </div>
      {contextMenu ? (
        <div
          className="file-context-menu editor-tab-context-menu"
          role="menu"
          style={{ left: contextMenu.x, top: contextMenu.y }}
          onClick={(event) => event.stopPropagation()}
          onContextMenu={(event) => event.preventDefault()}
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              closeBuffer(contextMenu.key);
              setContextMenu(null);
            }}
          >
            Close
          </button>
          <button
            type="button"
            role="menuitem"
            disabled={openBuffers.length <= 1}
            onClick={() => {
              closeOtherBuffers(contextMenu.key);
              setContextMenu(null);
            }}
          >
            Close Others
          </button>
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              closeAllBuffers();
              setContextMenu(null);
            }}
          >
            Close All
          </button>
        </div>
      ) : null}
    </div>
  );
}
