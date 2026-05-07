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
}

export const GHOSTTY_COMMIT = "0deaac08ed1a95330346afabbad03da701708331";

export function createGhosttyTerminalEngine(cols: number, rows: number): GhosttyTerminalEngine {
  return new CanvasBackedTerminalEngine(cols, rows);
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
