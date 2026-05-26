export interface GhosttyCell {
  text: string;
  foreground?: string;
  background?: string;
  bold?: boolean;
  italic?: boolean;
  inverse?: boolean;
  underline?: boolean;
  strikethrough?: boolean;
  invisible?: boolean;
}

export interface GhosttyRenderColors {
  background: string;
  foreground: string;
  cursor?: string;
  palette: string[];
}

export interface GhosttyCursorState {
  row: number;
  col: number;
  visible: boolean;
}

export interface GhosttySnapshot {
  rows: string[];
  styledRows?: GhosttyCell[][];
  colors?: GhosttyRenderColors;
  cursor: GhosttyCursorState;
  title?: string;
  bell: boolean;
}

export interface GhosttyTerminalEngine {
  resize(cols: number, rows: number, cellWidthPx?: number, cellHeightPx?: number): void;
  ingest(bytes: Uint8Array): void;
  snapshot(): GhosttySnapshot;
  takeDirtyRows(): number[];
  reset(): void;
  dispose?(): void;
}

export const GHOSTTY_COMMIT = "0deaac08ed1a95330346afabbad03da701708331";

const GHOSTTY_WASM_PATH = "/ghostty-vt.wasm";
const GHOSTTY_SUCCESS = 0;
const REPLAY_BUFFER_LIMIT_BYTES = 8 * 1024 * 1024;
const GHOSTTY_RENDER_STATE_DIRTY_FALSE = 0;
const GHOSTTY_RENDER_STATE_DIRTY_PARTIAL = 1;
const GHOSTTY_RENDER_STATE_DIRTY_FULL = 2;
const GHOSTTY_RENDER_STATE_DATA_DIRTY = 3;
const GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR = 4;
const GHOSTTY_RENDER_STATE_DATA_COLORS = 9;
const GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE = 11;
const GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE = 14;
const GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X = 15;
const GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y = 16;
const GHOSTTY_RENDER_STATE_OPTION_DIRTY = 0;
const GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY = 1;
const GHOSTTY_RENDER_STATE_ROW_DATA_CELLS = 3;
const GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY = 0;
const GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE = 2;
const GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN = 3;
const GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF = 4;

interface GhosttyTerminalEngineOptions {
  enableWasm?: boolean;
  wasmURL?: string;
  onBackendReady?: () => void;
}

interface GhosttyTypeField {
  offset: number;
  type: string;
}

interface GhosttyTypeLayout {
  [typeName: string]: {
    size: number;
    fields: Record<string, GhosttyTypeField>;
  };
}

interface GhosttyWasmExports extends WebAssembly.Exports {
  memory: WebAssembly.Memory;
  ghostty_type_json: () => number;
  ghostty_free: (allocatorPtr: number, ptr: number, len: number) => void;
  ghostty_wasm_alloc_opaque: () => number;
  ghostty_wasm_free_opaque: (ptr: number) => void;
  ghostty_wasm_alloc_u8_array: (len: number) => number;
  ghostty_wasm_free_u8_array: (ptr: number, len: number) => void;
  ghostty_wasm_alloc_usize: () => number;
  ghostty_wasm_free_usize: (ptr: number) => void;
  ghostty_terminal_new: (allocatorPtr: number, termPtrPtr: number, optsPtr: number) => number;
  ghostty_terminal_free: (termPtr: number) => void;
  ghostty_terminal_reset: (termPtr: number) => void;
  ghostty_terminal_resize: (
    termPtr: number,
    cols: number,
    rows: number,
    cellWidthPx: number,
    cellHeightPx: number
  ) => number;
  ghostty_terminal_vt_write: (termPtr: number, dataPtr: number, len: number) => void;
  ghostty_terminal_get: (termPtr: number, key: number, outPtr: number) => number;
  ghostty_render_state_new: (allocatorPtr: number, renderStatePtrPtr: number) => number;
  ghostty_render_state_update: (renderStatePtr: number, termPtr: number) => number;
  ghostty_render_state_get: (renderStatePtr: number, key: number, outPtr: number) => number;
  ghostty_render_state_set: (renderStatePtr: number, key: number, valuePtr: number) => number;
  ghostty_render_state_free: (renderStatePtr: number) => void;
  ghostty_render_state_row_iterator_new: (
    allocatorPtr: number,
    rowIteratorPtrPtr: number
  ) => number;
  ghostty_render_state_row_iterator_next: (rowIteratorPtr: number) => boolean;
  ghostty_render_state_row_iterator_free: (rowIteratorPtr: number) => void;
  ghostty_render_state_row_get: (rowIteratorPtr: number, key: number, outPtr: number) => number;
  ghostty_render_state_row_set: (rowIteratorPtr: number, key: number, valuePtr: number) => number;
  ghostty_render_state_row_cells_new: (allocatorPtr: number, rowCellsPtrPtr: number) => number;
  ghostty_render_state_row_cells_next: (rowCellsPtr: number) => boolean;
  ghostty_render_state_row_cells_get: (rowCellsPtr: number, key: number, outPtr: number) => number;
  ghostty_render_state_row_cells_free: (rowCellsPtr: number) => void;
}

interface GhosttyWasmModule {
  exports: GhosttyWasmExports;
  layout: GhosttyTypeLayout;
}

let wasmModulePromise: Promise<GhosttyWasmModule> | null = null;

export function createGhosttyTerminalEngine(
  cols: number,
  rows: number,
  options: GhosttyTerminalEngineOptions = {}
): GhosttyTerminalEngine {
  return new HybridGhosttyTerminalEngine(cols, rows, options);
}

class HybridGhosttyTerminalEngine implements GhosttyTerminalEngine {
  private readonly fallback: CanvasBackedTerminalEngine;
  private readonly pendingReplay: Uint8Array[] = [];
  private readonly onBackendReady?: () => void;
  private wasm: GhosttyWasmTerminalEngine | null = null;
  private replayByteCount = 0;
  private replayOverflowed = false;
  private disposed = false;
  private cols: number;
  private rows: number;

  constructor(cols: number, rows: number, options: GhosttyTerminalEngineOptions) {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.fallback = new CanvasBackedTerminalEngine(this.cols, this.rows);
    this.onBackendReady = options.onBackendReady;

    if (options.enableWasm ?? true) {
      this.loadWasmBackend(options.wasmURL ?? GHOSTTY_WASM_PATH);
    }
  }

  resize(cols: number, rows: number, cellWidthPx?: number, cellHeightPx?: number): void {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.fallback.resize(this.cols, this.rows);
    this.wasm?.resize(this.cols, this.rows, cellWidthPx, cellHeightPx);
  }

  ingest(bytes: Uint8Array): void {
    this.fallback.ingest(bytes);
    if (this.wasm) {
      this.wasm.ingest(bytes);
      return;
    }

    if (this.replayOverflowed) {
      return;
    }
    if (this.replayByteCount + bytes.byteLength > REPLAY_BUFFER_LIMIT_BYTES) {
      this.pendingReplay.length = 0;
      this.replayByteCount = 0;
      this.replayOverflowed = true;
      return;
    }

    const copy = new Uint8Array(bytes.byteLength);
    copy.set(bytes);
    this.pendingReplay.push(copy);
    this.replayByteCount += copy.byteLength;
  }

  snapshot(): GhosttySnapshot {
    return this.wasm?.snapshot() ?? this.fallback.snapshot();
  }

  takeDirtyRows(): number[] {
    return this.wasm?.takeDirtyRows() ?? this.fallback.takeDirtyRows();
  }

  reset(): void {
    this.fallback.reset();
    this.wasm?.reset();
    this.pendingReplay.length = 0;
    this.replayByteCount = 0;
    this.replayOverflowed = false;
  }

  dispose(): void {
    this.disposed = true;
    this.pendingReplay.length = 0;
    this.wasm?.dispose();
    this.wasm = null;
  }

  private loadWasmBackend(wasmURL: string): void {
    if (!canLoadGhosttyWasm()) {
      return;
    }

    loadGhosttyWasmModule(wasmURL)
      .then((module) => {
        if (this.disposed || this.replayOverflowed) {
          return;
        }
        const engine = new GhosttyWasmTerminalEngine(module, this.cols, this.rows);
        for (const bytes of this.pendingReplay) {
          engine.ingest(bytes);
        }
        this.pendingReplay.length = 0;
        this.replayByteCount = 0;
        this.wasm = engine;
        this.onBackendReady?.();
      })
      .catch(() => {
        this.pendingReplay.length = 0;
        this.replayByteCount = 0;
        this.replayOverflowed = true;
      });
  }
}

class GhosttyWasmTerminalEngine implements GhosttyTerminalEngine {
  private readonly module: GhosttyWasmModule;
  private readonly decoder = new TextDecoder("utf-8");
  private termPtr: number;
  private renderStatePtr: number;
  private rowStrings: string[];
  private styledRows: GhosttyCell[][];
  private colors: GhosttyRenderColors | undefined;
  private pendingDirtyRows: number[] = [];
  private needsRenderStateSync = true;
  private forceFullDirty = true;
  private graphemeBufferPtr = 0;
  private graphemeBufferCodepoints = 0;
  private cols: number;
  private rows: number;
  private bell = false;

  constructor(module: GhosttyWasmModule, cols: number, rows: number) {
    this.module = module;
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.termPtr = this.createTerminal(this.cols, this.rows);
    this.renderStatePtr = this.createRenderState();
    this.rowStrings = this.createEmptyRows();
    this.styledRows = this.createEmptyStyledRows();
  }

  resize(cols: number, rows: number, cellWidthPx = 8, cellHeightPx = 20): void {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    const result = this.exports.ghostty_terminal_resize(
      this.termPtr,
      this.cols,
      this.rows,
      Math.max(1, Math.round(cellWidthPx)),
      Math.max(1, Math.round(cellHeightPx))
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_terminal_resize failed with result ${result}`);
    }
    this.rowStrings = this.createEmptyRows();
    this.styledRows = this.createEmptyStyledRows();
    this.markRenderStateDirty();
  }

  ingest(bytes: Uint8Array): void {
    if (bytes.byteLength === 0) {
      return;
    }
    if (bytes.includes(7)) {
      this.bell = true;
    }
    const dataPtr = this.exports.ghostty_wasm_alloc_u8_array(bytes.byteLength);
    try {
      new Uint8Array(this.memory.buffer).set(bytes, dataPtr);
      this.exports.ghostty_terminal_vt_write(this.termPtr, dataPtr, bytes.byteLength);
      this.needsRenderStateSync = true;
    } finally {
      this.exports.ghostty_wasm_free_u8_array(dataPtr, bytes.byteLength);
    }
  }

  snapshot(): GhosttySnapshot {
    this.syncRenderState();
    return {
      rows: [...this.rowStrings],
      styledRows: this.styledRows.map((row) => row.map((cell) => ({ ...cell }))),
      colors: this.colors ? { ...this.colors, palette: [...this.colors.palette] } : undefined,
      cursor: this.getCursorState(),
      title: this.getTerminalString(12),
      bell: this.bell
    };
  }

  takeDirtyRows(): number[] {
    this.syncRenderState();
    const rows = this.pendingDirtyRows;
    this.pendingDirtyRows = [];
    return rows;
  }

  reset(): void {
    this.exports.ghostty_terminal_reset(this.termPtr);
    this.bell = false;
    this.rowStrings = this.createEmptyRows();
    this.styledRows = this.createEmptyStyledRows();
    this.markRenderStateDirty();
  }

  dispose(): void {
    if (this.graphemeBufferPtr !== 0) {
      this.exports.ghostty_wasm_free_u8_array(this.graphemeBufferPtr, this.graphemeBufferCodepoints * 4);
      this.graphemeBufferPtr = 0;
      this.graphemeBufferCodepoints = 0;
    }
    if (this.renderStatePtr !== 0) {
      this.exports.ghostty_render_state_free(this.renderStatePtr);
      this.renderStatePtr = 0;
    }
    if (this.termPtr !== 0) {
      this.exports.ghostty_terminal_free(this.termPtr);
      this.termPtr = 0;
    }
  }

  private get exports(): GhosttyWasmExports {
    return this.module.exports;
  }

  private get memory(): WebAssembly.Memory {
    return this.exports.memory;
  }

  private createTerminal(cols: number, rows: number): number {
    const optionsLayout = this.layoutFor("GhosttyTerminalOptions");
    const optsPtr = this.exports.ghostty_wasm_alloc_u8_array(optionsLayout.size);
    const termPtrPtr = this.exports.ghostty_wasm_alloc_opaque();

    try {
      new Uint8Array(this.memory.buffer, optsPtr, optionsLayout.size).fill(0);
      const optsView = new DataView(this.memory.buffer, optsPtr, optionsLayout.size);
      this.setField(optsView, "GhosttyTerminalOptions", "cols", cols);
      this.setField(optsView, "GhosttyTerminalOptions", "rows", rows);
      this.setField(optsView, "GhosttyTerminalOptions", "max_scrollback", 0);

      const result = this.exports.ghostty_terminal_new(0, termPtrPtr, optsPtr);
      if (result !== GHOSTTY_SUCCESS) {
        throw new Error(`ghostty_terminal_new failed with result ${result}`);
      }

      return new DataView(this.memory.buffer).getUint32(termPtrPtr, true);
    } finally {
      this.exports.ghostty_wasm_free_u8_array(optsPtr, optionsLayout.size);
      this.exports.ghostty_wasm_free_opaque(termPtrPtr);
    }
  }

  private createRenderState(): number {
    const renderStatePtrPtr = this.exports.ghostty_wasm_alloc_opaque();
    try {
      const result = this.exports.ghostty_render_state_new(0, renderStatePtrPtr);
      if (result !== GHOSTTY_SUCCESS) {
        throw new Error(`ghostty_render_state_new failed with result ${result}`);
      }
      return new DataView(this.memory.buffer).getUint32(renderStatePtrPtr, true);
    } finally {
      this.exports.ghostty_wasm_free_opaque(renderStatePtrPtr);
    }
  }

  private syncRenderState(): void {
    if (!this.needsRenderStateSync && !this.forceFullDirty) {
      return;
    }

    const updateResult = this.exports.ghostty_render_state_update(
      this.renderStatePtr,
      this.termPtr
    );
    if (updateResult !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_update failed with result ${updateResult}`);
    }

    const dirtyState = this.getRenderStateU32(GHOSTTY_RENDER_STATE_DATA_DIRTY);
    if (dirtyState === GHOSTTY_RENDER_STATE_DIRTY_FALSE && !this.forceFullDirty) {
      this.needsRenderStateSync = false;
      return;
    }
    if (
      dirtyState !== GHOSTTY_RENDER_STATE_DIRTY_PARTIAL &&
      dirtyState !== GHOSTTY_RENDER_STATE_DIRTY_FULL &&
      !this.forceFullDirty
    ) {
      throw new Error(`Unexpected ghostty render-state dirty value ${dirtyState}`);
    }

    const shouldRebuildAll =
      this.forceFullDirty || dirtyState === GHOSTTY_RENDER_STATE_DIRTY_FULL;
    const changedRows = this.readChangedRows(shouldRebuildAll);
    this.pendingDirtyRows = mergeSortedRows(this.pendingDirtyRows, changedRows);
    this.clearRenderStateDirty();
    this.forceFullDirty = false;
    this.needsRenderStateSync = false;
  }

  private readChangedRows(shouldRebuildAll: boolean): number[] {
    const rowIteratorPtrPtr = this.exports.ghostty_wasm_alloc_opaque();
    const rowCellsPtrPtr = this.exports.ghostty_wasm_alloc_opaque();
    const rowDirtyPtr = this.exports.ghostty_wasm_alloc_u8_array(1);
    const cellGraphemeLenPtr = this.exports.ghostty_wasm_alloc_u8_array(4);
    const cellStylePtr = this.exports.ghostty_wasm_alloc_u8_array(this.layoutFor("GhosttyStyle").size);
    const cleanRowPtr = this.exports.ghostty_wasm_alloc_u8_array(1);
    const changedRows: number[] = [];

    try {
      this.colors = this.readRenderColors();
      const pointerView = new DataView(this.memory.buffer);
      pointerView.setUint32(rowIteratorPtrPtr, 0, true);
      pointerView.setUint32(rowCellsPtrPtr, 0, true);
      this.createRowIterator(rowIteratorPtrPtr);
      this.createRowCells(rowCellsPtrPtr);
      const rowIteratorPtr = new DataView(this.memory.buffer).getUint32(rowIteratorPtrPtr, true);
      const rowCellsPtr = new DataView(this.memory.buffer).getUint32(rowCellsPtrPtr, true);
      this.populateRowIterator(rowIteratorPtrPtr);
      new Uint8Array(this.memory.buffer)[cleanRowPtr] = 0;

      let rowIndex = 0;
      while (
        rowIndex < this.rows &&
        this.exports.ghostty_render_state_row_iterator_next(rowIteratorPtr)
      ) {
        const rowDirty = shouldRebuildAll || this.getRowDirty(rowIteratorPtr, rowDirtyPtr);
        if (rowDirty) {
          this.populateRowCells(rowIteratorPtr, rowCellsPtrPtr);
          const cells = this.readRowCells(rowCellsPtr, cellGraphemeLenPtr, cellStylePtr);
          this.styledRows[rowIndex] = cells;
          this.rowStrings[rowIndex] = cells.map((cell) => cell.text || " ").join("").padEnd(this.cols, " ");
          changedRows.push(rowIndex);
          this.setRowClean(rowIteratorPtr, cleanRowPtr);
        }
        rowIndex += 1;
      }
    } finally {
      const view = new DataView(this.memory.buffer);
      const rowCellsPtr = view.getUint32(rowCellsPtrPtr, true);
      const rowIteratorPtr = view.getUint32(rowIteratorPtrPtr, true);
      if (rowCellsPtr !== 0) {
        this.exports.ghostty_render_state_row_cells_free(rowCellsPtr);
      }
      if (rowIteratorPtr !== 0) {
        this.exports.ghostty_render_state_row_iterator_free(rowIteratorPtr);
      }
      this.exports.ghostty_wasm_free_u8_array(cleanRowPtr, 1);
      this.exports.ghostty_wasm_free_u8_array(cellStylePtr, this.layoutFor("GhosttyStyle").size);
      this.exports.ghostty_wasm_free_u8_array(cellGraphemeLenPtr, 4);
      this.exports.ghostty_wasm_free_u8_array(rowDirtyPtr, 1);
      this.exports.ghostty_wasm_free_opaque(rowCellsPtrPtr);
      this.exports.ghostty_wasm_free_opaque(rowIteratorPtrPtr);
    }

    return changedRows;
  }

  private createRowIterator(rowIteratorPtrPtr: number): void {
    const result = this.exports.ghostty_render_state_row_iterator_new(0, rowIteratorPtrPtr);
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_row_iterator_new failed with result ${result}`);
    }
  }

  private populateRowIterator(rowIteratorPtrPtr: number): void {
    const result = this.exports.ghostty_render_state_get(
      this.renderStatePtr,
      GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR,
      rowIteratorPtrPtr
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_get row iterator failed with result ${result}`);
    }
  }

  private createRowCells(rowCellsPtrPtr: number): void {
    const result = this.exports.ghostty_render_state_row_cells_new(0, rowCellsPtrPtr);
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_row_cells_new failed with result ${result}`);
    }
  }

  private populateRowCells(rowIteratorPtr: number, rowCellsPtrPtr: number): void {
    const result = this.exports.ghostty_render_state_row_get(
      rowIteratorPtr,
      GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
      rowCellsPtrPtr
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_row_get cells failed with result ${result}`);
    }
  }

  private getRowDirty(rowIteratorPtr: number, rowDirtyPtr: number): boolean {
    new Uint8Array(this.memory.buffer)[rowDirtyPtr] = 0;
    const result = this.exports.ghostty_render_state_row_get(
      rowIteratorPtr,
      GHOSTTY_RENDER_STATE_ROW_DATA_DIRTY,
      rowDirtyPtr
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_row_get dirty failed with result ${result}`);
    }
    return new Uint8Array(this.memory.buffer)[rowDirtyPtr] !== 0;
  }

  private setRowClean(rowIteratorPtr: number, cleanRowPtr: number): void {
    const result = this.exports.ghostty_render_state_row_set(
      rowIteratorPtr,
      GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY,
      cleanRowPtr
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_render_state_row_set dirty failed with result ${result}`);
    }
  }

  private readRowCells(
    rowCellsPtr: number,
    cellGraphemeLenPtr: number,
    cellStylePtr: number
  ): GhosttyCell[] {
    const cells: GhosttyCell[] = [];
    let col = 0;
    while (
      col < this.cols &&
      this.exports.ghostty_render_state_row_cells_next(rowCellsPtr)
    ) {
      const text = this.readCellText(rowCellsPtr, cellGraphemeLenPtr);
      cells.push({
        text: text === "" ? " " : text,
        ...this.readCellStyle(rowCellsPtr, cellStylePtr)
      });
      col += 1;
    }
    while (cells.length < this.cols) {
      cells.push({ text: " " });
    }
    return cells;
  }

  private readCellStyle(rowCellsPtr: number, cellStylePtr: number): Omit<GhosttyCell, "text"> {
    new Uint8Array(this.memory.buffer, cellStylePtr, this.layoutFor("GhosttyStyle").size).fill(0);
    const result = this.exports.ghostty_render_state_row_cells_get(
      rowCellsPtr,
      GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE,
      cellStylePtr
    );
    if (result !== GHOSTTY_SUCCESS) {
      return {};
    }
    const view = new DataView(this.memory.buffer, cellStylePtr, this.layoutFor("GhosttyStyle").size);
    const styleLayout = this.layoutFor("GhosttyStyle");
    const underline = view.getInt32(styleLayout.fields.underline.offset, true) !== 0;
    return {
      foreground: this.readStyleColor(view, styleLayout.fields.fg_color.offset),
      background: this.readStyleColor(view, styleLayout.fields.bg_color.offset),
      bold: view.getUint8(styleLayout.fields.bold.offset) !== 0 || undefined,
      italic: view.getUint8(styleLayout.fields.italic.offset) !== 0 || undefined,
      inverse: view.getUint8(styleLayout.fields.inverse.offset) !== 0 || undefined,
      invisible: view.getUint8(styleLayout.fields.invisible.offset) !== 0 || undefined,
      strikethrough: view.getUint8(styleLayout.fields.strikethrough.offset) !== 0 || undefined,
      underline: underline || undefined
    };
  }

  private readStyleColor(view: DataView, styleColorOffset: number): string | undefined {
    const styleColorLayout = this.layoutFor("GhosttyStyleColor");
    const tag = view.getUint32(styleColorOffset + styleColorLayout.fields.tag.offset, true);
    const valueOffset = styleColorOffset + styleColorLayout.fields.value.offset;
    if (tag === 1) {
      const paletteIndex = view.getUint8(valueOffset);
      return this.colors?.palette[paletteIndex];
    }
    if (tag === 2) {
      return rgbToCss(
        view.getUint8(valueOffset),
        view.getUint8(valueOffset + 1),
        view.getUint8(valueOffset + 2)
      );
    }
    return undefined;
  }

  private readCellText(rowCellsPtr: number, cellGraphemeLenPtr: number): string {
    new DataView(this.memory.buffer).setUint32(cellGraphemeLenPtr, 0, true);
    const lenResult = this.exports.ghostty_render_state_row_cells_get(
      rowCellsPtr,
      GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN,
      cellGraphemeLenPtr
    );
    if (lenResult !== GHOSTTY_SUCCESS) {
      return "";
    }

    const graphemeLen = new DataView(this.memory.buffer).getUint32(cellGraphemeLenPtr, true);
    if (graphemeLen === 0) {
      return "";
    }

    this.ensureGraphemeBuffer(graphemeLen);
    const bufResult = this.exports.ghostty_render_state_row_cells_get(
      rowCellsPtr,
      GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
      this.graphemeBufferPtr
    );
    if (bufResult !== GHOSTTY_SUCCESS) {
      return "";
    }

    const view = new DataView(this.memory.buffer, this.graphemeBufferPtr, graphemeLen * 4);
    const codepoints: number[] = [];
    for (let index = 0; index < graphemeLen; index += 1) {
      const codepoint = view.getUint32(index * 4, true);
      if (codepoint > 0 && codepoint <= 0x10ffff) {
        codepoints.push(codepoint);
      }
    }
    return codepoints.length === 0 ? "" : String.fromCodePoint(...codepoints);
  }

  private ensureGraphemeBuffer(codepoints: number): void {
    if (this.graphemeBufferPtr !== 0 && this.graphemeBufferCodepoints >= codepoints) {
      return;
    }
    if (this.graphemeBufferPtr !== 0) {
      this.exports.ghostty_wasm_free_u8_array(
        this.graphemeBufferPtr,
        this.graphemeBufferCodepoints * 4
      );
    }
    this.graphemeBufferCodepoints = Math.max(32, codepoints);
    this.graphemeBufferPtr = this.exports.ghostty_wasm_alloc_u8_array(
      this.graphemeBufferCodepoints * 4
    );
  }

  private getRenderStateU32(key: number): number {
    return this.withOutPointer(4, (ptr) => {
      const result = this.exports.ghostty_render_state_get(this.renderStatePtr, key, ptr);
      if (result !== GHOSTTY_SUCCESS) {
        return 0;
      }
      return new DataView(this.memory.buffer, ptr, 4).getUint32(0, true);
    });
  }

  private readRenderColors(): GhosttyRenderColors | undefined {
    const colorsLayout = this.layoutFor("GhosttyRenderStateColors");
    return this.withOutPointer(colorsLayout.size, (ptr) => {
      const result = this.exports.ghostty_render_state_get(
        this.renderStatePtr,
        GHOSTTY_RENDER_STATE_DATA_COLORS,
        ptr
      );
      if (result !== GHOSTTY_SUCCESS) {
        return undefined;
      }
      const view = new DataView(this.memory.buffer, ptr, colorsLayout.size);
      const paletteOffset = colorsLayout.fields.palette.offset;
      const palette: string[] = [];
      for (let index = 0; index < 256; index += 1) {
        palette.push(this.readRgb(view, paletteOffset + index * 3));
      }
      const cursorHasValue = view.getUint8(colorsLayout.fields.cursor_has_value.offset) !== 0;
      return {
        background: this.readRgb(view, colorsLayout.fields.background.offset),
        foreground: this.readRgb(view, colorsLayout.fields.foreground.offset),
        cursor: cursorHasValue ? this.readRgb(view, colorsLayout.fields.cursor.offset) : undefined,
        palette
      };
    });
  }

  private readRgb(view: DataView, offset: number): string {
    return rgbToCss(view.getUint8(offset), view.getUint8(offset + 1), view.getUint8(offset + 2));
  }

  private getRenderStateU16(key: number): number {
    return this.withOutPointer(2, (ptr) => {
      const result = this.exports.ghostty_render_state_get(this.renderStatePtr, key, ptr);
      if (result !== GHOSTTY_SUCCESS) {
        return 0;
      }
      return new DataView(this.memory.buffer, ptr, 2).getUint16(0, true);
    });
  }

  private getRenderStateBool(key: number): boolean {
    return this.withOutPointer(1, (ptr) => {
      const result = this.exports.ghostty_render_state_get(this.renderStatePtr, key, ptr);
      return result === GHOSTTY_SUCCESS && new DataView(this.memory.buffer, ptr, 1).getUint8(0) !== 0;
    });
  }

  private clearRenderStateDirty(): void {
    this.withOutPointer(4, (ptr) => {
      new DataView(this.memory.buffer, ptr, 4).setUint32(
        0,
        GHOSTTY_RENDER_STATE_DIRTY_FALSE,
        true
      );
      const result = this.exports.ghostty_render_state_set(
        this.renderStatePtr,
        GHOSTTY_RENDER_STATE_OPTION_DIRTY,
        ptr
      );
      if (result !== GHOSTTY_SUCCESS) {
        throw new Error(`ghostty_render_state_set dirty failed with result ${result}`);
      }
    });
  }

  private getCursorState(): GhosttyCursorState {
    const hasViewportCursor = this.getRenderStateBool(
      GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_HAS_VALUE
    );
    if (hasViewportCursor) {
      return {
        row: clamp(
          this.getRenderStateU16(GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_Y),
          0,
          this.rows - 1
        ),
        col: clamp(
          this.getRenderStateU16(GHOSTTY_RENDER_STATE_DATA_CURSOR_VIEWPORT_X),
          0,
          this.cols - 1
        ),
        visible: this.getRenderStateBool(GHOSTTY_RENDER_STATE_DATA_CURSOR_VISIBLE)
      };
    }

    return {
      row: clamp(this.getTerminalU16(4), 0, this.rows - 1),
      col: clamp(this.getTerminalU16(3), 0, this.cols - 1),
      visible: false
    };
  }

  private getTerminalU16(key: number): number {
    return this.withOutPointer(2, (ptr) => {
      const result = this.exports.ghostty_terminal_get(this.termPtr, key, ptr);
      if (result !== GHOSTTY_SUCCESS) {
        return 0;
      }
      return new DataView(this.memory.buffer, ptr, 2).getUint16(0, true);
    });
  }

  private getTerminalString(key: number): string | undefined {
    return this.withOutPointer(8, (ptr) => {
      const result = this.exports.ghostty_terminal_get(this.termPtr, key, ptr);
      if (result !== GHOSTTY_SUCCESS) {
        return undefined;
      }
      const view = new DataView(this.memory.buffer, ptr, 8);
      const stringPtr = view.getUint32(0, true);
      const stringLen = view.getUint32(4, true);
      if (stringPtr === 0 || stringLen === 0) {
        return undefined;
      }
      return this.decoder.decode(new Uint8Array(this.memory.buffer, stringPtr, stringLen));
    });
  }

  private withOutPointer<T>(byteCount: number, body: (ptr: number) => T): T {
    const ptr = this.exports.ghostty_wasm_alloc_u8_array(byteCount);
    try {
      new Uint8Array(this.memory.buffer, ptr, byteCount).fill(0);
      return body(ptr);
    } finally {
      this.exports.ghostty_wasm_free_u8_array(ptr, byteCount);
    }
  }

  private setField(view: DataView, structName: string, fieldName: string, value: number): void {
    const field = this.fieldInfo(structName, fieldName);
    switch (field.type) {
      case "bool":
      case "u8":
        view.setUint8(field.offset, value);
        break;
      case "u16":
        view.setUint16(field.offset, value, true);
        break;
      case "enum":
      case "u32":
        view.setUint32(field.offset, value, true);
        break;
      case "u64":
        view.setBigUint64(field.offset, BigInt(value), true);
        break;
      default:
        throw new Error(`Unsupported Ghostty field type ${field.type}`);
    }
  }

  private fieldInfo(structName: string, fieldName: string): GhosttyTypeField {
    const field = this.layoutFor(structName).fields[fieldName];
    if (!field) {
      throw new Error(`Missing Ghostty field layout for ${structName}.${fieldName}`);
    }
    return field;
  }

  private layoutFor(structName: string): GhosttyTypeLayout[string] {
    const layout = this.module.layout[structName];
    if (!layout) {
      throw new Error(`Missing Ghostty type layout for ${structName}`);
    }
    return layout;
  }

  private createEmptyRows(): string[] {
    return Array.from({ length: this.rows }, () => " ".repeat(this.cols));
  }

  private createEmptyStyledRows(): GhosttyCell[][] {
    return Array.from({ length: this.rows }, () =>
      Array.from({ length: this.cols }, () => ({ text: " " }))
    );
  }

  private markRenderStateDirty(): void {
    this.pendingDirtyRows = [];
    this.forceFullDirty = true;
    this.needsRenderStateSync = true;
  }
}

class CanvasBackedTerminalEngine implements GhosttyTerminalEngine {
  private cols: number;
  private rows: number;
  private lines: string[];
  private cursor: GhosttyCursorState = { row: 0, col: 0, visible: true };
  private dirtyRows = new Set<number>();
  private decoder = new TextDecoder("utf-8");
  private bell = false;

  constructor(cols: number, rows: number) {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.lines = Array.from({ length: this.rows }, () => "");
    this.markAllDirty();
  }

  resize(cols: number, rows: number): void {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    if (this.lines.length > this.rows) {
      this.lines = this.lines.slice(this.lines.length - this.rows);
    }
    while (this.lines.length < this.rows) {
      this.lines.push("");
    }
    this.cursor.row = Math.min(this.cursor.row, this.rows - 1);
    this.cursor.col = Math.min(this.cursor.col, this.cols - 1);
    this.markAllDirty();
  }

  ingest(bytes: Uint8Array): void {
    const text = this.decoder.decode(bytes, { stream: true });
    for (const char of text) {
      this.ingestCharacter(char);
    }
  }

  snapshot(): GhosttySnapshot {
    return {
      rows: [...this.lines],
      cursor: { ...this.cursor },
      bell: this.bell
    };
  }

  takeDirtyRows(): number[] {
    const rows = [...this.dirtyRows].sort((left, right) => left - right);
    this.dirtyRows.clear();
    return rows;
  }

  reset(): void {
    this.lines = Array.from({ length: this.rows }, () => "");
    this.cursor = { row: 0, col: 0, visible: true };
    this.decoder = new TextDecoder("utf-8");
    this.bell = false;
    this.markAllDirty();
  }

  private ingestCharacter(char: string): void {
    switch (char) {
      case "\u0007":
        this.bell = true;
        return;
      case "\r":
        this.cursor.col = 0;
        this.dirtyRows.add(this.cursor.row);
        return;
      case "\n":
        this.advanceLine();
        return;
      case "\b":
        this.cursor.col = Math.max(0, this.cursor.col - 1);
        this.dirtyRows.add(this.cursor.row);
        return;
      case "\t":
        this.writeText(" ".repeat(4 - (this.cursor.col % 4)));
        return;
      default:
        if (char < " ") {
          return;
        }
        this.writeText(char);
    }
  }

  private writeText(text: string): void {
    for (const char of text) {
      if (this.cursor.col >= this.cols) {
        this.advanceLine();
      }
      const rowText = this.lines[this.cursor.row] ?? "";
      const padded = rowText.padEnd(this.cursor.col, " ");
      this.lines[this.cursor.row] =
        padded.slice(0, this.cursor.col) + char + padded.slice(this.cursor.col + 1);
      this.cursor.col += 1;
      this.dirtyRows.add(this.cursor.row);
    }
  }

  private advanceLine(): void {
    this.dirtyRows.add(this.cursor.row);
    this.cursor.col = 0;
    if (this.cursor.row >= this.rows - 1) {
      this.lines.shift();
      this.lines.push("");
      this.markAllDirty();
      return;
    }
    this.cursor.row += 1;
    this.dirtyRows.add(this.cursor.row);
  }

  private markAllDirty(): void {
    for (let row = 0; row < this.rows; row += 1) {
      this.dirtyRows.add(row);
    }
  }
}

function canLoadGhosttyWasm(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof fetch === "function" &&
    typeof WebAssembly !== "undefined" &&
    typeof WebAssembly.instantiate === "function"
  );
}

function loadGhosttyWasmModule(wasmURL: string): Promise<GhosttyWasmModule> {
  wasmModulePromise ??= instantiateGhosttyWasm(wasmURL);
  return wasmModulePromise;
}

async function instantiateGhosttyWasm(wasmURL: string): Promise<GhosttyWasmModule> {
  const response = await fetch(wasmURL);
  if (!response.ok) {
    throw new Error(`Failed to load ${wasmURL}: ${response.status}`);
  }
  const wasmBytes = await response.arrayBuffer();
  let wasmExports: GhosttyWasmExports | null = null;
  const wasmModule = await WebAssembly.instantiate(wasmBytes, {
    env: {
      log: (ptr: number, len: number) => {
        if (!wasmExports) {
          return;
        }
        const text = new TextDecoder().decode(new Uint8Array(wasmExports.memory.buffer, ptr, len));
        console.debug("[ghostty-vt]", text);
      }
    }
  });
  const exports = wasmModule.instance.exports as GhosttyWasmExports;
  wasmExports = exports;
  const jsonPtr = exports.ghostty_type_json();
  const layoutText = readCString(exports.memory, jsonPtr);
  return {
    exports,
    layout: JSON.parse(layoutText) as GhosttyTypeLayout
  };
}

function readCString(memory: WebAssembly.Memory, ptr: number): string {
  const bytes = new Uint8Array(memory.buffer);
  let end = ptr;
  while (end < bytes.byteLength && bytes[end] !== 0) {
    end += 1;
  }
  return new TextDecoder().decode(bytes.subarray(ptr, end));
}

function rgbToCss(red: number, green: number, blue: number): string {
  return `rgb(${red}, ${green}, ${blue})`;
}

function clamp(value: number, lowerBound: number, upperBound: number): number {
  return Math.max(lowerBound, Math.min(value, upperBound));
}

function mergeSortedRows(left: number[], right: number[]): number[] {
  if (left.length === 0) {
    return right;
  }
  if (right.length === 0) {
    return left;
  }
  return [...new Set([...left, ...right])].sort((a, b) => a - b);
}
