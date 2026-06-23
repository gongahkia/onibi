import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import type { IDisposable } from "@xterm/xterm";
import type { TerminalWS } from "./ws";
import "@xterm/xterm/css/xterm.css";

export type TerminalHandle = {
  term: Terminal;
  fit: FitAddon;
};

export function createTerminal(container: HTMLElement): TerminalHandle {
  const term = new Terminal({
    fontFamily: "Menlo, Monaco, monospace",
    fontSize: 14,
    cursorBlink: true,
    convertEol: false,
    scrollback: 5000,
    theme: {
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
    }
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.open(container);
  fit.fit();
  return { term, fit };
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
  const send = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(() => {
      fit.fit();
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

function binaryStringBytes(value: string): Uint8Array {
  const bytes = new Uint8Array(value.length);
  for (let i = 0; i < value.length; i += 1) {
    bytes[i] = value.charCodeAt(i) & 0xff;
  }
  return bytes;
}
