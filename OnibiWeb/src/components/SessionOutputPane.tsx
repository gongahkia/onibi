import { useEffect, useRef } from "react";
import { createGhosttyTerminalEngine, type GhosttyTerminalEngine } from "../lib/ghosttyTerminal";
import type { OutputEntry } from "../store/sessionStore";

interface SessionOutputPaneProps {
  entries: OutputEntry[];
  onTerminalInput: (data: string) => void;
  onTerminalPaste: (text: string) => void;
  onTerminalResize: (cols: number, rows: number) => void;
}

const FONT_FAMILY =
  '"JetBrains Mono Nerd Font", "JetBrainsMono Nerd Font", "JetBrainsMonoNL Nerd Font", "Symbols Nerd Font Mono", "MesloLGS NF", "Hack Nerd Font Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, "Cascadia Mono", "Segoe UI Mono", "Roboto Mono", monospace';
const FONT_SIZE = 14;
const LINE_HEIGHT = 20;
const CELL_WIDTH = 8.4;

export function SessionOutputPane({
  entries,
  onTerminalInput,
  onTerminalPaste,
  onTerminalResize
}: SessionOutputPaneProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const engineRef = useRef<GhosttyTerminalEngine | null>(null);
  const renderStateRef = useRef<TerminalRenderState>({});
  const renderSchedulerRef = useRef<TerminalRenderScheduler | null>(null);
  const lastRenderedChunkIdRef = useRef<string | null>(null);
  const onTerminalInputRef = useRef(onTerminalInput);
  const onTerminalPasteRef = useRef(onTerminalPaste);
  const onTerminalResizeRef = useRef(onTerminalResize);
  const lastReportedSizeRef = useRef<{ cols: number; rows: number } | null>(null);

  useEffect(() => {
    onTerminalInputRef.current = onTerminalInput;
  }, [onTerminalInput]);

  useEffect(() => {
    onTerminalPasteRef.current = onTerminalPaste;
  }, [onTerminalPaste]);

  useEffect(() => {
    onTerminalResizeRef.current = onTerminalResize;
  }, [onTerminalResize]);

  useEffect(() => {
    const container = containerRef.current;
    const canvas = canvasRef.current;
    if (!container || !canvas) {
      return;
    }

    renderSchedulerRef.current = createTerminalRenderScheduler(
      () => canvasRef.current,
      () => engineRef.current,
      renderStateRef.current
    );

    const reportSize = () => {
      const rect = container.getBoundingClientRect();
      const cols = Math.max(1, Math.floor(rect.width / CELL_WIDTH));
      const rows = Math.max(1, Math.floor(rect.height / LINE_HEIGHT));
      const pixelRatio = window.devicePixelRatio || 1;
      canvas.width = Math.floor(rect.width * pixelRatio);
      canvas.height = Math.floor(rect.height * pixelRatio);
      canvas.style.width = `${rect.width}px`;
      canvas.style.height = `${rect.height}px`;
      const context = canvas.getContext("2d");
      if (context) {
        context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0);
      }

      if (!engineRef.current) {
        engineRef.current = createGhosttyTerminalEngine(cols, rows, {
          onBackendReady: () => {
            renderSchedulerRef.current?.schedule(true);
          }
        });
      } else {
        engineRef.current.resize(cols, rows);
      }
      renderSchedulerRef.current?.schedule(true);

      const lastReportedSize = lastReportedSizeRef.current;
      if (!lastReportedSize || lastReportedSize.cols !== cols || lastReportedSize.rows !== rows) {
        lastReportedSizeRef.current = { cols, rows };
        onTerminalResizeRef.current(cols, rows);
      }
    };

    const focusTerminal = () => {
      container.focus();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      const data = keyEventToTerminalInput(event);
      if (!data) {
        return;
      }
      event.preventDefault();
      onTerminalInputRef.current(data);
    };
    const onBeforeInput = (event: InputEvent) => {
      if (event.data) {
        event.preventDefault();
        onTerminalInputRef.current(event.data);
      }
    };
    const onPaste = (event: ClipboardEvent) => {
      const pastedText = event.clipboardData?.getData("text") ?? "";
      if (routeTerminalPaste(pastedText, onTerminalInputRef.current, onTerminalPasteRef.current)) {
        event.preventDefault();
      }
    };

    const resizeObserver = new ResizeObserver(reportSize);
    resizeObserver.observe(container);
    container.addEventListener("pointerdown", focusTerminal);
    container.addEventListener("touchstart", focusTerminal, { passive: true });
    container.addEventListener("keydown", onKeyDown);
    container.addEventListener("beforeinput", onBeforeInput as EventListener);
    container.addEventListener("paste", onPaste);
    reportSize();
    container.focus();

    return () => {
      resizeObserver.disconnect();
      container.removeEventListener("pointerdown", focusTerminal);
      container.removeEventListener("touchstart", focusTerminal);
      container.removeEventListener("keydown", onKeyDown);
      container.removeEventListener("beforeinput", onBeforeInput as EventListener);
      container.removeEventListener("paste", onPaste);
      renderSchedulerRef.current?.dispose();
      renderSchedulerRef.current = null;
      engineRef.current?.dispose?.();
      engineRef.current = null;
      renderStateRef.current = {};
      lastRenderedChunkIdRef.current = null;
      lastReportedSizeRef.current = null;
    };
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    const engine = engineRef.current;
    if (!canvas || !engine) {
      return;
    }

    if (entries.length === 0) {
      engine.reset();
      lastRenderedChunkIdRef.current = null;
      renderSchedulerRef.current?.schedule(true);
      return;
    }

    const lastRenderedChunkID = lastRenderedChunkIdRef.current;
    let startIndex = 0;

    if (lastRenderedChunkID) {
      const lastIndex = entries.findIndex((entry) => entry.id === lastRenderedChunkID);
      if (lastIndex >= 0) {
        startIndex = lastIndex + 1;
      } else {
        engine.reset();
      }
    } else {
      engine.reset();
    }

    for (let index = startIndex; index < entries.length; index += 1) {
      const bytes = entries[index].bytes ?? new TextEncoder().encode(entries[index].text);
      engine.ingest(bytes);
    }

    lastRenderedChunkIdRef.current = entries[entries.length - 1].id;
    renderSchedulerRef.current?.schedule();
  }, [entries]);

  return (
    <section className="mf-output-pane">
      <div
        className="mf-terminal-container mf-canvas-terminal"
        ref={containerRef}
        tabIndex={0}
        spellCheck={false}
      >
        <canvas ref={canvasRef} />
      </div>
    </section>
  );
}

export function routeTerminalPaste(
  text: string,
  onTerminalInput: (data: string) => void,
  onTerminalPaste: (text: string) => void
): boolean {
  if (!text) {
    return false;
  }
  if (!text.includes("\n") && !text.includes("\r") && Array.from(text).length === 1) {
    onTerminalInput(text);
    return true;
  }
  onTerminalPaste(text);
  return true;
}

export interface TerminalRenderState {
  lastCursor?: {
    row: number;
    col: number;
    visible: boolean;
  };
}

interface TerminalRenderScheduler {
  schedule(forceFullRender?: boolean): void;
  cancel(): void;
  dispose(): void;
}

export function createTerminalRenderScheduler(
  getCanvas: () => HTMLCanvasElement | null,
  getEngine: () => GhosttyTerminalEngine | null,
  renderState: TerminalRenderState,
  requestFrame: (callback: FrameRequestCallback) => number = window.requestAnimationFrame,
  cancelFrame: (handle: number) => void = window.cancelAnimationFrame
): TerminalRenderScheduler {
  let frameHandle: number | null = null;
  let pendingForceFullRender = false;
  let disposed = false;

  const render = () => {
    frameHandle = null;
    if (disposed) {
      return;
    }
    const canvas = getCanvas();
    const engine = getEngine();
    if (!canvas || !engine) {
      pendingForceFullRender = false;
      return;
    }
    const forceFullRender = pendingForceFullRender;
    pendingForceFullRender = false;
    renderTerminal(canvas, engine, renderState, forceFullRender);
  };

  return {
    schedule(forceFullRender = false) {
      if (disposed) {
        return;
      }
      pendingForceFullRender = pendingForceFullRender || forceFullRender;
      if (frameHandle !== null) {
        return;
      }
      frameHandle = requestFrame(render);
    },
    cancel() {
      if (frameHandle !== null) {
        cancelFrame(frameHandle);
        frameHandle = null;
      }
      pendingForceFullRender = false;
    },
    dispose() {
      disposed = true;
      this.cancel();
    }
  };
}

export function renderTerminal(
  canvas: HTMLCanvasElement,
  engine: GhosttyTerminalEngine,
  renderState: TerminalRenderState = {},
  forceFullRender = false
): void {
  const context = canvas.getContext("2d");
  if (!context) {
    return;
  }
  const snapshot = engine.snapshot();
  const dirtyRows = forceFullRender ? snapshot.rows.map((_, index) => index) : engine.takeDirtyRows();
  if (forceFullRender) {
    engine.takeDirtyRows();
  }
  const rowSet = new Set(dirtyRows);
  if (renderState.lastCursor?.visible) {
    rowSet.add(renderState.lastCursor.row);
  }
  if (snapshot.cursor.visible) {
    rowSet.add(snapshot.cursor.row);
  }
  const rows = [...rowSet]
    .filter((row) => row >= 0 && row < snapshot.rows.length)
    .sort((left, right) => left - right);

  if (forceFullRender) {
    context.fillStyle = "#0d1117";
    context.fillRect(0, 0, canvas.clientWidth, canvas.clientHeight);
  }
  context.font = `${FONT_SIZE}px ${FONT_FAMILY}`;
  context.textBaseline = "top";
  context.fillStyle = "#0d1117";
  for (const row of rows) {
    context.fillRect(0, row * LINE_HEIGHT, canvas.clientWidth, LINE_HEIGHT);
    context.fillStyle = "#f3f4f6";
    context.fillText(snapshot.rows[row] ?? "", 0, row * LINE_HEIGHT + 2);
    context.fillStyle = "#0d1117";
  }

  if (snapshot.cursor.visible) {
    context.fillStyle = "#f3f4f6";
    context.fillRect(
      snapshot.cursor.col * CELL_WIDTH,
      snapshot.cursor.row * LINE_HEIGHT + 2,
      2,
      LINE_HEIGHT - 4
    );
  }
  renderState.lastCursor = { ...snapshot.cursor };
}

function keyEventToTerminalInput(event: KeyboardEvent): string | null {
  if (event.metaKey) {
    return null;
  }
  if (event.ctrlKey && event.key.length === 1) {
    const code = event.key.toUpperCase().charCodeAt(0) - 64;
    if (code > 0 && code < 32) {
      return String.fromCharCode(code);
    }
  }
  switch (event.key) {
    case "Enter":
      return "\r";
    case "Tab":
      return "\t";
    case "Backspace":
      return "\u007f";
    case "Escape":
      return "\u001b";
    case "ArrowUp":
      return "\u001b[A";
    case "ArrowDown":
      return "\u001b[B";
    case "ArrowRight":
      return "\u001b[C";
    case "ArrowLeft":
      return "\u001b[D";
    default:
      return event.key.length === 1 ? event.key : null;
  }
}
