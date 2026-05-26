import { useEffect, useRef } from "react";
import {
  createGhosttyTerminalEngine,
  type GhosttyCell,
  type GhosttyRenderColors,
  type GhosttyTerminalEngine
} from "../lib/ghosttyTerminal";
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
const SELECTION_COLOR = "rgba(56, 139, 253, 0.35)";
const DEFAULT_BACKGROUND = "#0d1117";
const DEFAULT_FOREGROUND = "#f3f4f6";
const TEXT_TOP_OFFSET = 2;
const METRIC_SAMPLE = "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm";
const BACKLOG_MAX_BATCH_BYTES = 64 * 1024;
const BACKLOG_MAX_BATCH_ENTRIES = 256;

export interface TerminalViewMetrics {
  cellWidth: number;
  lineHeight: number;
  fontSize: number;
  textTopOffset: number;
}

export const FALLBACK_TERMINAL_METRICS: TerminalViewMetrics = {
  cellWidth: CELL_WIDTH,
  lineHeight: LINE_HEIGHT,
  fontSize: FONT_SIZE,
  textTopOffset: TEXT_TOP_OFFSET
};

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
  const backlogProcessorRef = useRef<TerminalBacklogProcessor | null>(null);
  const metricsRef = useRef<TerminalViewMetrics>(FALLBACK_TERMINAL_METRICS);
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
      () => metricsRef.current,
      renderStateRef.current
    );

    const reportSize = () => {
      const rect = container.getBoundingClientRect();
      const pixelRatio = window.devicePixelRatio || 1;
      canvas.width = Math.floor(rect.width * pixelRatio);
      canvas.height = Math.floor(rect.height * pixelRatio);
      canvas.style.width = `${rect.width}px`;
      canvas.style.height = `${rect.height}px`;
      const context = canvas.getContext("2d");
      if (context) {
        context.setTransform(pixelRatio, 0, 0, pixelRatio, 0, 0);
        context.font = terminalFont(FALLBACK_TERMINAL_METRICS);
        metricsRef.current = measureTerminalMetrics(context);
      }
      const metrics = metricsRef.current;
      const { cols, rows } = calculateTerminalSize(rect.width, rect.height, metrics);

      if (!engineRef.current) {
        engineRef.current = createGhosttyTerminalEngine(cols, rows, {
          onBackendReady: () => {
            renderSchedulerRef.current?.schedule(true);
          }
        });
      } else {
        engineRef.current.resize(cols, rows, metrics.cellWidth, metrics.lineHeight);
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
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "c") {
        const selectedText = copyTextForSelection(
          renderStateRef.current.lastRows ?? [],
          renderStateRef.current.selection
        );
        if (selectedText !== null) {
          event.preventDefault();
          const writeClipboardText = navigator.clipboard?.writeText?.bind(navigator.clipboard);
          if (writeClipboardText) {
            void writeClipboardText(selectedText).catch(() => undefined);
          }
          return;
        }
      }
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
    const onPointerDown = (event: PointerEvent) => {
      if (event.button !== 0) {
        return;
      }
      focusTerminal();
      const cell = cellFromPointerEvent(container, event, metricsRef.current);
      const previousSelection = renderStateRef.current.selection;
      renderStateRef.current.selection = {
        anchor: cell,
        head: cell
      };
      markSelectionRowsDirty(renderStateRef.current, previousSelection, renderStateRef.current.selection);
      renderSchedulerRef.current?.schedule();
      container.setPointerCapture?.(event.pointerId);
    };
    const onPointerMove = (event: PointerEvent) => {
      const selection = renderStateRef.current.selection;
      if (!selection || event.buttons !== 1) {
        return;
      }
      const nextSelection = {
        anchor: selection.anchor,
        head: cellFromPointerEvent(container, event, metricsRef.current)
      };
      markSelectionRowsDirty(renderStateRef.current, selection, nextSelection);
      renderStateRef.current.selection = nextSelection;
      renderSchedulerRef.current?.schedule();
    };
    const onPointerUp = (event: PointerEvent) => {
      const selection = renderStateRef.current.selection;
      if (!selection) {
        return;
      }
      const nextSelection = {
        anchor: selection.anchor,
        head: cellFromPointerEvent(container, event, metricsRef.current)
      };
      const finalSelection = hasSelectionRange(nextSelection) ? nextSelection : undefined;
      markSelectionRowsDirty(renderStateRef.current, selection, finalSelection);
      renderStateRef.current.selection = finalSelection;
      renderSchedulerRef.current?.schedule();
      container.releasePointerCapture?.(event.pointerId);
    };

    const resizeObserver = new ResizeObserver(reportSize);
    resizeObserver.observe(container);
    container.addEventListener("pointerdown", onPointerDown);
    container.addEventListener("pointermove", onPointerMove);
    container.addEventListener("pointerup", onPointerUp);
    container.addEventListener("touchstart", focusTerminal, { passive: true });
    container.addEventListener("keydown", onKeyDown);
    container.addEventListener("beforeinput", onBeforeInput as EventListener);
    container.addEventListener("paste", onPaste);
    reportSize();
    container.focus();

    return () => {
      resizeObserver.disconnect();
      container.removeEventListener("pointerdown", onPointerDown);
      container.removeEventListener("pointermove", onPointerMove);
      container.removeEventListener("pointerup", onPointerUp);
      container.removeEventListener("touchstart", focusTerminal);
      container.removeEventListener("keydown", onKeyDown);
      container.removeEventListener("beforeinput", onBeforeInput as EventListener);
      container.removeEventListener("paste", onPaste);
      renderSchedulerRef.current?.dispose();
      renderSchedulerRef.current = null;
      backlogProcessorRef.current?.cancel();
      backlogProcessorRef.current = null;
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
      backlogProcessorRef.current?.cancel();
      backlogProcessorRef.current = null;
      engine.reset();
      lastRenderedChunkIdRef.current = null;
      renderStateRef.current.selection = undefined;
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
        backlogProcessorRef.current?.cancel();
        engine.reset();
        renderSchedulerRef.current?.schedule(true);
      }
    } else {
      backlogProcessorRef.current?.cancel();
      engine.reset();
      renderSchedulerRef.current?.schedule(true);
    }

    if (startIndex >= entries.length) {
      return;
    }

    backlogProcessorRef.current?.cancel();
    const processor = createTerminalBacklogProcessor({
      entries,
      startIndex,
      ingestEntry: (entry) => {
        const bytes = entry.bytes ?? new TextEncoder().encode(entry.text);
        engine.ingest(bytes);
      },
      onEntryIngested: (entry) => {
        lastRenderedChunkIdRef.current = entry.id;
      },
      scheduleRender: () => renderSchedulerRef.current?.schedule(),
      onComplete: () => {
        if (backlogProcessorRef.current === processor) {
          backlogProcessorRef.current = null;
        }
      }
    });
    backlogProcessorRef.current = processor;
    processor.start();
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
  selection?: TerminalSelection;
  lastRows?: string[];
  extraDirtyRows?: Set<number>;
}

export interface TerminalCellPosition {
  row: number;
  col: number;
}

export interface TerminalSelection {
  anchor: TerminalCellPosition;
  head: TerminalCellPosition;
}

interface TerminalRenderScheduler {
  schedule(forceFullRender?: boolean): void;
  cancel(): void;
  dispose(): void;
}

interface TerminalBacklogProcessor {
  start(): void;
  cancel(): void;
}

interface TerminalBacklogProcessorOptions {
  entries: OutputEntry[];
  startIndex: number;
  ingestEntry: (entry: OutputEntry) => void;
  onEntryIngested: (entry: OutputEntry) => void;
  scheduleRender: () => void;
  onComplete?: () => void;
  maxBatchBytes?: number;
  maxBatchEntries?: number;
  requestFrame?: (callback: FrameRequestCallback) => number;
  cancelFrame?: (handle: number) => void;
}

export function createTerminalBacklogProcessor({
  entries,
  startIndex,
  ingestEntry,
  onEntryIngested,
  scheduleRender,
  onComplete,
  maxBatchBytes = BACKLOG_MAX_BATCH_BYTES,
  maxBatchEntries = BACKLOG_MAX_BATCH_ENTRIES,
  requestFrame = window.requestAnimationFrame,
  cancelFrame = window.cancelAnimationFrame
}: TerminalBacklogProcessorOptions): TerminalBacklogProcessor {
  let index = startIndex;
  let frameHandle: number | null = null;
  let cancelled = false;

  const processBatch = () => {
    frameHandle = null;
    if (cancelled) {
      return;
    }
    let batchBytes = 0;
    let batchEntries = 0;
    while (index < entries.length && batchEntries < maxBatchEntries) {
      const entry = entries[index];
      const entryBytes = entry.bytes?.byteLength ?? new TextEncoder().encode(entry.text).byteLength;
      ingestEntry(entry);
      onEntryIngested(entry);
      index += 1;
      batchEntries += 1;
      batchBytes += entryBytes;
      if (batchBytes >= maxBatchBytes) {
        break;
      }
    }
    scheduleRender();
    if (index < entries.length) {
      frameHandle = requestFrame(processBatch);
    } else {
      onComplete?.();
    }
  };

  return {
    start() {
      if (cancelled || frameHandle !== null) {
        return;
      }
      frameHandle = requestFrame(processBatch);
    },
    cancel() {
      cancelled = true;
      if (frameHandle !== null) {
        cancelFrame(frameHandle);
        frameHandle = null;
      }
    }
  };
}

export function createTerminalRenderScheduler(
  getCanvas: () => HTMLCanvasElement | null,
  getEngine: () => GhosttyTerminalEngine | null,
  getMetrics: () => TerminalViewMetrics,
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
    renderTerminal(canvas, engine, renderState, forceFullRender, getMetrics());
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
  forceFullRender = false,
  metrics: TerminalViewMetrics = FALLBACK_TERMINAL_METRICS
): void {
  const context = canvas.getContext("2d");
  if (!context) {
    return;
  }
  const snapshot = engine.snapshot();
  renderState.lastRows = snapshot.rows;
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
  for (const row of renderState.extraDirtyRows ?? []) {
    rowSet.add(row);
  }
  renderState.extraDirtyRows?.clear();
  const rows = [...rowSet]
    .filter((row) => row >= 0 && row < snapshot.rows.length)
    .sort((left, right) => left - right);
  const colors = snapshot.colors ?? defaultRenderColors();

  if (forceFullRender) {
    context.fillStyle = colors.background;
    context.fillRect(0, 0, canvas.clientWidth, canvas.clientHeight);
  }
  context.font = terminalFont(metrics);
  context.textBaseline = "top";
  for (const row of rows) {
    context.fillStyle = colors.background;
    context.fillRect(0, row * metrics.lineHeight, canvas.clientWidth, metrics.lineHeight);
    const styledRow = snapshot.styledRows?.[row];
    if (styledRow) {
      drawStyledRowBackgrounds(context, row, styledRow, colors, metrics);
      drawSelectionOverlay(context, row, renderState.selection, styledRow.length, metrics);
      drawStyledRowText(context, row, styledRow, colors, metrics);
    } else {
      drawSelectionOverlay(context, row, renderState.selection, snapshot.rows[row]?.length ?? 0, metrics);
      context.fillStyle = colors.foreground;
      context.fillText(snapshot.rows[row] ?? "", 0, row * metrics.lineHeight + metrics.textTopOffset);
    }
  }

  if (snapshot.cursor.visible) {
    context.fillStyle = colors.cursor ?? colors.foreground;
    context.fillRect(
      snapshot.cursor.col * metrics.cellWidth,
      snapshot.cursor.row * metrics.lineHeight + metrics.textTopOffset,
      2,
      metrics.lineHeight - metrics.textTopOffset * 2
    );
  }
  renderState.lastCursor = { ...snapshot.cursor };
}

export function cellFromPoint(
  clientX: number,
  clientY: number,
  bounds: Pick<DOMRect, "left" | "top" | "width" | "height">,
  rowCount: number,
  colCount: number,
  metrics: TerminalViewMetrics = FALLBACK_TERMINAL_METRICS
): TerminalCellPosition {
  const col = clamp(Math.floor((clientX - bounds.left) / metrics.cellWidth), 0, Math.max(0, colCount - 1));
  const row = clamp(Math.floor((clientY - bounds.top) / metrics.lineHeight), 0, Math.max(0, rowCount - 1));
  return { row, col };
}

export function normalizedSelection(
  selection: TerminalSelection | undefined
): { start: TerminalCellPosition; end: TerminalCellPosition } | null {
  if (!selection || !hasSelectionRange(selection)) {
    return null;
  }
  const anchorBeforeHead =
    selection.anchor.row < selection.head.row ||
    (selection.anchor.row === selection.head.row && selection.anchor.col <= selection.head.col);
  return anchorBeforeHead
    ? { start: selection.anchor, end: selection.head }
    : { start: selection.head, end: selection.anchor };
}

export function copyTextForSelection(
  rows: string[],
  selection: TerminalSelection | undefined
): string | null {
  const normalized = normalizedSelection(selection);
  if (!normalized) {
    return null;
  }
  const copiedRows: string[] = [];
  for (let row = normalized.start.row; row <= normalized.end.row; row += 1) {
    const rowText = rows[row] ?? "";
    const startCol = row === normalized.start.row ? normalized.start.col : 0;
    const endCol = row === normalized.end.row ? normalized.end.col : rowText.length - 1;
    copiedRows.push(rowText.slice(startCol, endCol + 1).replace(/ +$/, ""));
  }
  return copiedRows.join("\n");
}

function cellFromPointerEvent(
  container: HTMLDivElement,
  event: Pick<PointerEvent, "clientX" | "clientY">,
  metrics: TerminalViewMetrics
): TerminalCellPosition {
  const bounds = container.getBoundingClientRect();
  const { cols, rows } = calculateTerminalSize(bounds.width, bounds.height, metrics);
  return cellFromPoint(event.clientX, event.clientY, bounds, rows, cols, metrics);
}

function hasSelectionRange(selection: TerminalSelection | undefined): selection is TerminalSelection {
  return Boolean(
    selection &&
      (selection.anchor.row !== selection.head.row || selection.anchor.col !== selection.head.col)
  );
}

function markSelectionRowsDirty(
  renderState: TerminalRenderState,
  previousSelection: TerminalSelection | undefined,
  nextSelection: TerminalSelection | undefined
): void {
  for (const row of selectedRows(previousSelection)) {
    markExtraDirtyRow(renderState, row);
  }
  for (const row of selectedRows(nextSelection)) {
    markExtraDirtyRow(renderState, row);
  }
}

function selectedRows(selection: TerminalSelection | undefined): number[] {
  const normalized = normalizedSelection(selection);
  if (!normalized) {
    return [];
  }
  const rows: number[] = [];
  for (let row = normalized.start.row; row <= normalized.end.row; row += 1) {
    rows.push(row);
  }
  return rows;
}

function markExtraDirtyRow(renderState: TerminalRenderState, row: number): void {
  renderState.extraDirtyRows ??= new Set<number>();
  renderState.extraDirtyRows.add(row);
}

function drawSelectionOverlay(
  context: CanvasRenderingContext2D,
  row: number,
  selection: TerminalSelection | undefined,
  colCount: number,
  metrics: TerminalViewMetrics
): void {
  const normalized = normalizedSelection(selection);
  if (!normalized || row < normalized.start.row || row > normalized.end.row) {
    return;
  }
  const startCol = row === normalized.start.row ? normalized.start.col : 0;
  const endCol = row === normalized.end.row ? normalized.end.col : Math.max(0, colCount - 1);
  context.fillStyle = SELECTION_COLOR;
  context.fillRect(
    startCol * metrics.cellWidth,
    row * metrics.lineHeight,
    (endCol - startCol + 1) * metrics.cellWidth,
    metrics.lineHeight
  );
  context.fillStyle = "#0d1117";
}

function drawStyledRowBackgrounds(
  context: CanvasRenderingContext2D,
  row: number,
  cells: GhosttyCell[],
  colors: GhosttyRenderColors,
  metrics: TerminalViewMetrics
): void {
  cells.forEach((cell, col) => {
    const resolved = resolveCellColors(cell, colors);
    if (resolved.background === colors.background) {
      return;
    }
    context.fillStyle = resolved.background;
    context.fillRect(
      col * metrics.cellWidth,
      row * metrics.lineHeight,
      metrics.cellWidth,
      metrics.lineHeight
    );
  });
}

function drawStyledRowText(
  context: CanvasRenderingContext2D,
  row: number,
  cells: GhosttyCell[],
  colors: GhosttyRenderColors,
  metrics: TerminalViewMetrics
): void {
  cells.forEach((cell, col) => {
    if (cell.invisible || cell.text === "") {
      return;
    }
    const resolved = resolveCellColors(cell, colors);
    context.font = terminalFont(metrics, cell);
    context.fillStyle = resolved.foreground;
    const x = col * metrics.cellWidth;
    const y = row * metrics.lineHeight + metrics.textTopOffset;
    context.fillText(cell.text, x, y);
    if (cell.underline) {
      context.fillRect(x, row * metrics.lineHeight + metrics.lineHeight - 3, metrics.cellWidth, 1);
    }
    if (cell.strikethrough) {
      context.fillRect(x, row * metrics.lineHeight + Math.floor(metrics.lineHeight / 2), metrics.cellWidth, 1);
    }
  });
  context.font = terminalFont(metrics);
}

function resolveCellColors(
  cell: GhosttyCell,
  colors: GhosttyRenderColors
): { foreground: string; background: string } {
  const foreground = cell.foreground ?? colors.foreground;
  const background = cell.background ?? colors.background;
  return cell.inverse
    ? { foreground: background, background: foreground }
    : { foreground, background };
}

function defaultRenderColors(): GhosttyRenderColors {
  return {
    background: DEFAULT_BACKGROUND,
    foreground: DEFAULT_FOREGROUND,
    cursor: DEFAULT_FOREGROUND,
    palette: []
  };
}

export function measureTerminalMetrics(context: CanvasRenderingContext2D): TerminalViewMetrics {
  const measured = context.measureText?.(METRIC_SAMPLE);
  const measuredCellWidth = measured?.width ? measured.width / METRIC_SAMPLE.length : 0;
  const fontBoxHeight =
    measured && Number.isFinite(measured.actualBoundingBoxAscent + measured.actualBoundingBoxDescent)
      ? measured.actualBoundingBoxAscent + measured.actualBoundingBoxDescent
      : 0;
  const lineHeight =
    fontBoxHeight > 0
      ? Math.max(Math.ceil(fontBoxHeight + 6), FALLBACK_TERMINAL_METRICS.lineHeight)
      : FALLBACK_TERMINAL_METRICS.lineHeight;
  return {
    cellWidth:
      Number.isFinite(measuredCellWidth) && measuredCellWidth > 0
        ? measuredCellWidth
        : FALLBACK_TERMINAL_METRICS.cellWidth,
    lineHeight,
    fontSize: FONT_SIZE,
    textTopOffset: Math.max(1, Math.floor((lineHeight - FONT_SIZE) / 2))
  };
}

export function calculateTerminalSize(
  width: number,
  height: number,
  metrics: TerminalViewMetrics
): { cols: number; rows: number } {
  return {
    cols: Math.max(1, Math.floor(width / metrics.cellWidth)),
    rows: Math.max(1, Math.floor(height / metrics.lineHeight))
  };
}

function terminalFont(metrics: TerminalViewMetrics, cell?: Pick<GhosttyCell, "bold" | "italic">): string {
  const style = [cell?.italic ? "italic" : "", cell?.bold ? "700" : ""].filter(Boolean).join(" ");
  return `${style ? `${style} ` : ""}${metrics.fontSize}px ${FONT_FAMILY}`;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

export function keyEventToTerminalInput(event: KeyboardEvent): string | null {
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
