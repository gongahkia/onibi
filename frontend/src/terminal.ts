import { FitAddon } from "@xterm/addon-fit";
import { ImageAddon } from "@xterm/addon-image";
import { Terminal } from "@xterm/xterm";
import type { IDisposable, ITheme } from "@xterm/xterm";
import type { TerminalWS } from "./ws";
import "@xterm/xterm/css/xterm.css";

export const terminalThemeNames = ["dark", "light"] as const;
export type TerminalThemeName = (typeof terminalThemeNames)[number];

export type TerminalHandle = {
  term: Terminal;
  fit: FitAddon;
};

const terminalFont = `"JetBrainsMono Nerd Font", "JetBrainsMono Nerd Font Mono", "JetBrains Mono", Menlo, Monaco, monospace`;
export const terminalFontSizeKey = "onibi-font-size";
export const terminalThemeKey = "onibi-theme";
export const defaultTerminalFontSize = 14;
const minTerminalFontSize = 10;
const maxTerminalFontSize = 22;
let loggedCrampedPortrait = false;

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

export const defaultTerminalTheme: TerminalThemeName = "dark";

export function isTerminalThemeName(value: string | null): value is TerminalThemeName {
  return terminalThemeNames.includes(value as TerminalThemeName);
}

const themeLabels: Record<TerminalThemeName, string> = {
  dark: "Dark",
  light: "Light"
};

export function terminalThemeLabel(theme: TerminalThemeName): string {
  return themeLabels[theme];
}

export function loadStoredTerminalTheme(storage: Storage): TerminalThemeName {
  const stored = storage.getItem(terminalThemeKey);
  if (isTerminalThemeName(stored)) {
    return stored;
  }
  storage.setItem(terminalThemeKey, defaultTerminalTheme);
  return defaultTerminalTheme;
}

export function createTerminal(
  container: HTMLElement,
  theme: TerminalThemeName = defaultTerminalTheme
): TerminalHandle {
  const term = new Terminal({
    fontFamily: terminalFont,
    fontSize: loadTerminalFontSize(),
    cursorBlink: true,
    convertEol: false,
    scrollback: 5000,
    theme: themes[theme]
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new ImageAddon());
  term.open(container);
  fit.fit();
  logCrampedPortraitCols(term);
  return { term, fit };
}

export function applyTerminalTheme(term: Terminal, theme: TerminalThemeName): void {
  term.options.theme = themes[theme];
}

export function clampTerminalFontSize(size: number): number {
  return Math.min(maxTerminalFontSize, Math.max(minTerminalFontSize, size));
}

export function loadTerminalFontSize(): number {
  const stored = window.localStorage.getItem(terminalFontSizeKey);
  if (stored === null) {
    return defaultTerminalFontSize;
  }
  const size = Number(stored);
  return Number.isFinite(size) ? clampTerminalFontSize(size) : defaultTerminalFontSize;
}

export function logCrampedPortraitCols(term: Terminal): void {
  if (
    loggedCrampedPortrait ||
    !window.matchMedia("(orientation: portrait)").matches ||
    term.cols >= 40
  ) {
    return;
  }
  loggedCrampedPortrait = true;
  console.warn(`[ONIBI] portrait cols=${term.cols}`);
}

export function attachTerminalIO(
  term: Terminal,
  ws: TerminalWS,
  enabled: () => boolean = () => true
): IDisposable {
  const data = term.onData((input) => {
    if (enabled()) {
      ws.sendText(input);
    }
  });
  const binary = term.onBinary((input) => {
    if (enabled()) {
      ws.sendBinary(binaryStringBytes(input));
    }
  });
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
      logCrampedPortraitCols(term);
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
