import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import type { IDisposable, ITheme } from "@xterm/xterm";
import type { TerminalWS } from "./ws";
import "@xterm/xterm/css/xterm.css";

export type TerminalThemeName = "dark" | "light";

export type TerminalHandle = {
  term: Terminal;
  fit: FitAddon;
};

const terminalFont = `"JetBrainsMono Nerd Font", "JetBrainsMono Nerd Font Mono", "JetBrains Mono", Menlo, Monaco, monospace`;

const themes: Record<TerminalThemeName, ITheme> = {
  dark: {
    background: "#090b0f",
    foreground: "#d7dde8",
    cursor: "#f4f7fb",
    selectionBackground: "#2f6feb66",
    black: "#141821",
    red: "#f47067",
    green: "#7ee787",
    yellow: "#f2cc60",
    blue: "#79c0ff",
    magenta: "#d2a8ff",
    cyan: "#76e3ea",
    white: "#d7dde8",
    brightBlack: "#6e7681",
    brightRed: "#ff7b72",
    brightGreen: "#8ddb8c",
    brightYellow: "#e3b341",
    brightBlue: "#a5d6ff",
    brightMagenta: "#d8b9ff",
    brightCyan: "#b3f0ff",
    brightWhite: "#ffffff"
  },
  light: {
    background: "#f6f8fa",
    foreground: "#18202b",
    cursor: "#0b1020",
    selectionBackground: "#0969da44",
    black: "#24292f",
    red: "#cf222e",
    green: "#116329",
    yellow: "#953800",
    blue: "#0969da",
    magenta: "#8250df",
    cyan: "#1b7c83",
    white: "#f6f8fa",
    brightBlack: "#57606a",
    brightRed: "#a40e26",
    brightGreen: "#1a7f37",
    brightYellow: "#9a6700",
    brightBlue: "#218bff",
    brightMagenta: "#a475f9",
    brightCyan: "#3192aa",
    brightWhite: "#ffffff"
  }
};

export function createTerminal(container: HTMLElement, theme: TerminalThemeName = "dark"): TerminalHandle {
  const term = new Terminal({
    fontFamily: terminalFont,
    fontSize: 14,
    cursorBlink: true,
    convertEol: false,
    scrollback: 5000,
    theme: themes[theme]
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();
  return { term, fit };
}

export function applyTerminalTheme(term: Terminal, theme: TerminalThemeName): void {
  term.options.theme = themes[theme];
}

export function attachTerminalIO(term: Terminal, ws: TerminalWS): IDisposable {
  const data = term.onData((input) => ws.sendText(input)); // text frame; server distinguishes JSON controls from input
  const binary = term.onBinary((input) => ws.sendBinary(binaryStringBytes(input))); // binary frame for opaque sequences
  return {
    dispose() {
      data.dispose();
      binary.dispose();
    }
  };
}

export function installViewportResize(term: Terminal, fit: FitAddon, ws: TerminalWS): IDisposable {
  let timer = 0;
  let lastRows = 0;
  let lastCols = 0;
  const send = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(() => {
      fit.fit();
      if (term.rows === lastRows && term.cols === lastCols) {
        return;
      }
      lastRows = term.rows;
      lastCols = term.cols;
      ws.sendResize(term.rows, term.cols);
    }, 200);
  };
  window.addEventListener("resize", send);
  window.visualViewport?.addEventListener("resize", send);
  send();
  return {
    dispose() {
      window.clearTimeout(timer);
      window.removeEventListener("resize", send);
      window.visualViewport?.removeEventListener("resize", send);
    }
  };
}

export function installTouchScroll(term: Terminal, root: HTMLElement): IDisposable {
  let lastY: number | undefined;
  let carry = 0;
  const linePx = () => Math.max(12, root.clientHeight / Math.max(1, term.rows));
  const start = (event: TouchEvent) => {
    if (event.touches.length !== 1) {
      lastY = undefined;
      carry = 0;
      return;
    }
    lastY = event.touches[0].clientY;
    carry = 0;
  };
  const move = (event: TouchEvent) => {
    if (lastY === undefined || event.touches.length !== 1) {
      return;
    }
    const y = event.touches[0].clientY;
    carry += lastY - y;
    lastY = y;
    const lines = Math.trunc(carry / linePx());
    if (lines === 0) {
      return;
    }
    term.scrollLines(lines);
    carry -= lines * linePx();
    event.preventDefault();
  };
  const end = () => {
    lastY = undefined;
    carry = 0;
  };
  root.addEventListener("touchstart", start, { passive: true });
  root.addEventListener("touchmove", move, { passive: false });
  root.addEventListener("touchend", end);
  root.addEventListener("touchcancel", end);
  return {
    dispose() {
      root.removeEventListener("touchstart", start);
      root.removeEventListener("touchmove", move);
      root.removeEventListener("touchend", end);
      root.removeEventListener("touchcancel", end);
    }
  };
}

function binaryStringBytes(value: string): Uint8Array {
  const bytes = new Uint8Array(value.length);
  for (let i = 0; i < value.length; i += 1) {
    bytes[i] = value.charCodeAt(i) & 0xff;
  }
  return bytes;
}
