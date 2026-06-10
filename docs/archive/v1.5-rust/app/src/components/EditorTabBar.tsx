import { useEffect, useState, type DragEvent, type MouseEvent } from "react";
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

interface DropIndicator {
  key: string;
  before: boolean;
}

const DRAG_MIME = "application/x-onibi-buffer-key";

export function EditorTabBar() {
  const openBuffers = useSessionStore((state) => state.openBuffers);
  const activeBufferKey = useSessionStore((state) => state.activeBufferKey);
  const dirtyKeys = useSessionStore((state) => state.dirtyBufferKeys);
  const setActiveBuffer = useSessionStore((state) => state.setActiveBuffer);
  const closeBuffer = useSessionStore((state) => state.closeBuffer);
  const closeOtherBuffers = useSessionStore((state) => state.closeOtherBuffers);
  const closeAllBuffers = useSessionStore((state) => state.closeAllBuffers);
  const reorderBuffer = useSessionStore((state) => state.reorderBuffer);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [draggingKey, setDraggingKey] = useState<string | null>(null);
  const [dropIndicator, setDropIndicator] = useState<DropIndicator | null>(null);

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

  function handleDragStart(event: DragEvent<HTMLDivElement>, key: string) {
    event.dataTransfer.setData(DRAG_MIME, key);
    event.dataTransfer.effectAllowed = "move";
    setDraggingKey(key);
  }

  function handleDragOver(event: DragEvent<HTMLDivElement>, key: string) {
    const dragged = event.dataTransfer.types.includes(DRAG_MIME);
    if (!dragged && !draggingKey) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    const rect = event.currentTarget.getBoundingClientRect();
    const before = event.clientX < rect.left + rect.width / 2;
    setDropIndicator((current) =>
      current?.key === key && current.before === before ? current : { key, before },
    );
  }

  function handleDragLeave(event: DragEvent<HTMLDivElement>) {
    // Only clear if we're truly leaving (not just moving between tabs)
    const related = event.relatedTarget as HTMLElement | null;
    if (!related || !event.currentTarget.contains(related)) {
      // do nothing; will be replaced on next dragover or cleared on drop/end
    }
  }

  function handleDrop(event: DragEvent<HTMLDivElement>, targetKey: string) {
    event.preventDefault();
    const fromKey = event.dataTransfer.getData(DRAG_MIME) || draggingKey;
    if (fromKey && fromKey !== targetKey) {
      const before = dropIndicator?.key === targetKey ? dropIndicator.before : false;
      reorderBuffer(fromKey, targetKey, before);
    }
    setDraggingKey(null);
    setDropIndicator(null);
  }

  function handleDragEnd() {
    setDraggingKey(null);
    setDropIndicator(null);
  }

  return (
    <div className="editor-tab-bar" role="tablist" aria-label="Open files">
      <div className="editor-tabs">
        {openBuffers.map((buffer) => {
          const key = bufferKey(buffer);
          const label = bufferLabel(buffer);
          const isActive = key === activeBufferKey;
          const isDirty = dirtyKeys.includes(key);
          const isDragging = draggingKey === key;
          const indicator =
            dropIndicator?.key === key && draggingKey && draggingKey !== key
              ? dropIndicator.before
                ? "drop-before"
                : "drop-after"
              : null;
          return (
            <div
              key={key}
              role="tab"
              aria-selected={isActive}
              className={`editor-tab ${isActive ? "active" : ""} ${
                isDirty ? "dirty" : ""
              } ${isDragging ? "dragging" : ""} ${indicator ?? ""}`}
              draggable
              title={buffer.type === "web" ? buffer.url : buffer.path}
              onClick={(event) => {
                event.stopPropagation();
                setActiveBuffer(key);
              }}
              onContextMenu={(event) => handleContextMenu(event, key)}
              onAuxClick={(event) => handleAuxClick(event, key)}
              onDragStart={(event) => handleDragStart(event, key)}
              onDragOver={(event) => handleDragOver(event, key)}
              onDragLeave={handleDragLeave}
              onDrop={(event) => handleDrop(event, key)}
              onDragEnd={handleDragEnd}
            >
              <i
                className={`codicon ${iconForBuffer(buffer)}`}
                aria-hidden="true"
              />
              <span className="editor-tab-label">
                {label}
                <span className="editor-tab-suffix">{suffixForBuffer(buffer)}</span>
              </span>
              {isDirty ? (
                <button
                  type="button"
                  className="editor-tab-close dirty-indicator"
                  aria-label={`Close ${label} (unsaved)`}
                  title="Unsaved changes — click to close"
                  onClick={(event) => {
                    event.stopPropagation();
                    closeBuffer(key);
                  }}
                >
                  <span className="dirty-dot-tab" aria-hidden="true" />
                  <i className="codicon codicon-close" aria-hidden="true" />
                </button>
              ) : (
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
              )}
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
