export interface GhosttyCell {
  text: string;
}

export interface GhosttyCursorState {
  row: number;
  col: number;
  visible: boolean;
}

export interface GhosttySnapshot {
  rows: string[];
  cursor: GhosttyCursorState;
  title?: string;
  bell: boolean;
}

export interface GhosttyTerminalEngine {
  resize(cols: number, rows: number): void;
  ingest(bytes: Uint8Array): void;
  snapshot(): GhosttySnapshot;
  takeDirtyRows(): number[];
  reset(): void;
  dispose?(): void;
}

export const GHOSTTY_COMMIT = "0deaac08ed1a95330346afabbad03da701708331";

const GHOSTTY_WASM_PATH = "/ghostty-vt.wasm";
const GHOSTTY_SUCCESS = 0;
const GHOSTTY_FORMATTER_FORMAT_PLAIN = 0;
const REPLAY_BUFFER_LIMIT_BYTES = 8 * 1024 * 1024;

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
  ghostty_formatter_terminal_new: (
    allocatorPtr: number,
    formatterPtrPtr: number,
    termPtr: number,
    optsPtr: number
  ) => number;
  ghostty_formatter_format_alloc: (
    formatterPtr: number,
    allocatorPtr: number,
    outPtrPtr: number,
    outLenPtr: number
  ) => number;
  ghostty_formatter_free: (formatterPtr: number) => void;
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

  resize(cols: number, rows: number): void {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.fallback.resize(this.cols, this.rows);
    this.wasm?.resize(this.cols, this.rows);
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
  private readonly dirtyRows = new Set<number>();
  private termPtr: number;
  private cols: number;
  private rows: number;
  private bell = false;

  constructor(module: GhosttyWasmModule, cols: number, rows: number) {
    this.module = module;
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    this.termPtr = this.createTerminal(this.cols, this.rows);
    this.markAllDirty();
  }

  resize(cols: number, rows: number): void {
    this.cols = Math.max(1, cols);
    this.rows = Math.max(1, rows);
    const result = this.exports.ghostty_terminal_resize(
      this.termPtr,
      this.cols,
      this.rows,
      8,
      20
    );
    if (result !== GHOSTTY_SUCCESS) {
      throw new Error(`ghostty_terminal_resize failed with result ${result}`);
    }
    this.markAllDirty();
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
      this.markAllDirty();
    } finally {
      this.exports.ghostty_wasm_free_u8_array(dataPtr, bytes.byteLength);
    }
  }

  snapshot(): GhosttySnapshot {
    return {
      rows: this.formatPlainRows(),
      cursor: {
        row: clamp(this.getTerminalU16(4), 0, this.rows - 1),
        col: clamp(this.getTerminalU16(3), 0, this.cols - 1),
        visible: this.getTerminalBool(7)
      },
      title: this.getTerminalString(12),
      bell: this.bell
    };
  }

  takeDirtyRows(): number[] {
    const rows = [...this.dirtyRows].sort((left, right) => left - right);
    this.dirtyRows.clear();
    return rows;
  }

  reset(): void {
    this.exports.ghostty_terminal_reset(this.termPtr);
    this.bell = false;
    this.markAllDirty();
  }

  dispose(): void {
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

  private formatPlainRows(): string[] {
    const formatterPtr = this.createFormatter();
    const outPtrPtr = this.exports.ghostty_wasm_alloc_opaque();
    const outLenPtr = this.exports.ghostty_wasm_alloc_usize();

    try {
      const result = this.exports.ghostty_formatter_format_alloc(
        formatterPtr,
        0,
        outPtrPtr,
        outLenPtr
      );
      if (result !== GHOSTTY_SUCCESS) {
        throw new Error(`ghostty_formatter_format_alloc failed with result ${result}`);
      }

      const view = new DataView(this.memory.buffer);
      const outPtr = view.getUint32(outPtrPtr, true);
      const outLen = view.getUint32(outLenPtr, true);
      const text = this.decoder.decode(new Uint8Array(this.memory.buffer, outPtr, outLen));
      this.exports.ghostty_free(0, outPtr, outLen);
      return normalizeSnapshotRows(text, this.rows);
    } finally {
      this.exports.ghostty_wasm_free_opaque(outPtrPtr);
      this.exports.ghostty_wasm_free_usize(outLenPtr);
      this.exports.ghostty_formatter_free(formatterPtr);
    }
  }

  private createFormatter(): number {
    const optionsLayout = this.layoutFor("GhosttyFormatterTerminalOptions");
    const optsPtr = this.exports.ghostty_wasm_alloc_u8_array(optionsLayout.size);
    const formatterPtrPtr = this.exports.ghostty_wasm_alloc_opaque();

    try {
      new Uint8Array(this.memory.buffer, optsPtr, optionsLayout.size).fill(0);
      const optsView = new DataView(this.memory.buffer, optsPtr, optionsLayout.size);
      this.setField(optsView, "GhosttyFormatterTerminalOptions", "size", optionsLayout.size);
      this.setField(
        optsView,
        "GhosttyFormatterTerminalOptions",
        "emit",
        GHOSTTY_FORMATTER_FORMAT_PLAIN
      );
      this.setField(optsView, "GhosttyFormatterTerminalOptions", "unwrap", 0);
      this.setField(optsView, "GhosttyFormatterTerminalOptions", "trim", 1);
      this.setNestedSizedStructFields(optsView);

      const result = this.exports.ghostty_formatter_terminal_new(
        0,
        formatterPtrPtr,
        this.termPtr,
        optsPtr
      );
      if (result !== GHOSTTY_SUCCESS) {
        throw new Error(`ghostty_formatter_terminal_new failed with result ${result}`);
      }

      return new DataView(this.memory.buffer).getUint32(formatterPtrPtr, true);
    } finally {
      this.exports.ghostty_wasm_free_u8_array(optsPtr, optionsLayout.size);
      this.exports.ghostty_wasm_free_opaque(formatterPtrPtr);
    }
  }

  private setNestedSizedStructFields(optsView: DataView): void {
    const extraOffset = this.fieldInfo("GhosttyFormatterTerminalOptions", "extra").offset;
    const extraLayout = this.layoutFor("GhosttyFormatterTerminalExtra");
    const extraSizeField = this.fieldInfo("GhosttyFormatterTerminalExtra", "size");
    optsView.setUint32(extraOffset + extraSizeField.offset, extraLayout.size, true);

    const screenOffset = this.fieldInfo("GhosttyFormatterTerminalExtra", "screen").offset;
    const screenLayout = this.layoutFor("GhosttyFormatterScreenExtra");
    const screenSizeField = this.fieldInfo("GhosttyFormatterScreenExtra", "size");
    optsView.setUint32(extraOffset + screenOffset + screenSizeField.offset, screenLayout.size, true);
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

  private getTerminalBool(key: number): boolean {
    return this.withOutPointer(1, (ptr) => {
      const result = this.exports.ghostty_terminal_get(this.termPtr, key, ptr);
      return result === GHOSTTY_SUCCESS && new DataView(this.memory.buffer, ptr, 1).getUint8(0) !== 0;
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

  private markAllDirty(): void {
    for (let row = 0; row < this.rows; row += 1) {
      this.dirtyRows.add(row);
    }
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
  const wasmModule = await WebAssembly.instantiate(wasmBytes, {
    env: {
      log: (ptr: number, len: number) => {
        const exports = wasmModule.instance.exports as GhosttyWasmExports;
        const text = new TextDecoder().decode(new Uint8Array(exports.memory.buffer, ptr, len));
        console.debug("[ghostty-vt]", text);
      }
    }
  });
  const exports = wasmModule.instance.exports as GhosttyWasmExports;
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

function normalizeSnapshotRows(text: string, rowCount: number): string[] {
  const rows = text.replace(/\r\n/g, "\n").split("\n");
  while (rows.length < rowCount) {
    rows.push("");
  }
  if (rows.length > rowCount) {
    return rows.slice(rows.length - rowCount);
  }
  return rows;
}

function clamp(value: number, lowerBound: number, upperBound: number): number {
  return Math.max(lowerBound, Math.min(value, upperBound));
}
